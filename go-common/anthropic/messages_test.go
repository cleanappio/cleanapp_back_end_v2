package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	u, _ := url.Parse(s.URL)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return NewClient("stub-key", "", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != messagesURL {
			t.Fatal("request left first-party endpoint")
		}
		r = r.Clone(r.Context())
		r.URL.Scheme = u.Scheme
		r.URL.Host = u.Host
		return transport.RoundTrip(r)
	})})
}

func TestMessagesHeadersSchemaAndOriginalImage(t *testing.T) {
	image := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "stub-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.URL.RawQuery != "" {
			t.Fatal("credential/version contract")
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("invalid request")
		}
		if b["model"] != DefaultModel || b["max_tokens"] != float64(4096) || b["system"] != "source-only rules" {
			t.Fatal("model/budget/system changed")
		}
		if _, ok := b["temperature"]; ok {
			t.Fatal("Haiku 5.5 must omit sampling controls")
		}
		if b["thinking"].(map[string]any)["type"] != "disabled" || b["output_config"].(map[string]any)["effort"] != "low" {
			t.Fatal("Haiku output budget could be consumed by thinking")
		}
		format := b["output_config"].(map[string]any)["format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Fatal("schema not sent")
		}
		blocks := b["messages"].([]any)[0].(map[string]any)["content"].([]any)
		source := blocks[1].(map[string]any)["source"].(map[string]any)
		if source["type"] != "base64" || source["media_type"] != "image/webp" || source["data"] == "" || blocks[0].(map[string]any)["text"] != "original" {
			t.Fatal("original image/text contract")
		}
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"thinking","text":"hidden"},{"type":"text","text":"{\"ok\":true}"}]}`)
	})
	schema := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "additionalProperties": false}
	answer, err := c.Generate(context.Background(), Input{System: "source-only rules", Text: "original", Image: image, JSONSchema: schema})
	if err != nil || answer != `{"ok":true}` {
		t.Fatalf("%q %v", answer, err)
	}
}

func TestRetriesAreBoundedAndErrorsDoNotEchoPayload(t *testing.T) {
	for _, status := range []int{400, 401, 429, 503, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, `private original stub-key`)
			})
			_, err := c.Generate(context.Background(), Input{Text: "private original"})
			want := 1
			if status == 429 || status >= 500 {
				want = 2
			}
			if calls != want || err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "stub-key") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestIncompleteRefusedMalformedAndOversizedResponsesFail(t *testing.T) {
	for _, body := range []string{
		`{"stop_reason":"max_tokens","content":[{"type":"text","text":"{}"}]}`,
		`{"stop_reason":"refusal","content":[{"type":"text","text":"private"}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"[]"}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"not-json"}]}`,
		strings.Repeat("x", maxResponseBytes+1),
	} {
		c := stubClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.Generate(context.Background(), Input{Text: "x", JSONSchema: map[string]any{"type": "object"}}); err == nil {
			t.Fatal("accepted invalid output")
		}
	}
}

func TestContextBudgetAndRedirectNeverLeakKey(t *testing.T) {
	calls := 0
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://untrusted.invalid/")
		w.WriteHeader(307)
	})
	if _, err := c.Generate(context.Background(), Input{Text: "x"}); err == nil || calls != 1 {
		t.Fatal("redirect followed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Generate(ctx, Input{Text: "x"}); err == nil || calls != 1 {
		t.Fatal("canceled request sent")
	}
	if _, err := c.Generate(context.Background(), Input{Text: "x", MaxTokens: 4097}); err == nil || calls != 1 {
		t.Fatal("unbounded output allowed")
	}
	c = stubClient(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) })
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Generate(ctx, Input{Text: "x"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline ignored")
	}
}

func TestThinkingSettingsAreDefaultModelOnly(t *testing.T) {
	for _, model := range []string{DefaultModel, "claude-sonnet-5-5"} {
		c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			_, thinking := body["thinking"]
			_, output := body["output_config"]
			if model == DefaultModel && (!thinking || !output) {
				t.Fatal("Haiku thinking not disabled without schema")
			}
			if model != DefaultModel && (thinking || output) {
				t.Fatal("model capability guessed")
			}
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"answer"}]}`)
		})
		c.model = model
		if _, err := c.Generate(context.Background(), Input{Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestImageSizeAndFormatRefuseBeforeSending(t *testing.T) {
	calls := 0
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, image := range [][]byte{[]byte("not an image"), make([]byte, maxEncodedImageBytes*3/4+1)} {
		if _, err := c.Generate(context.Background(), Input{Text: "x", Image: image}); err == nil {
			t.Fatal("invalid image sent")
		}
	}
	if calls != 0 {
		t.Fatal("invalid image reached provider")
	}
}
