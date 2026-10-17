package evidence

import (
	"encoding/json"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClaudeUsesOriginalSourceOnlyAndNeverFallsBack(t *testing.T) {
	old := requestLimiter
	requestLimiter = rate.NewLimiter(rate.Inf, 1)
	defer func() { requestLimiter = old }()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.anthropic.com" || r.URL.RawQuery != "" || r.Header.Get("x-api-key") != "stub-key" {
			t.Fatal("wrong endpoint/key transport")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid body")
		}
		if body["system"] != Prompt || body["output_config"] == nil {
			t.Fatal("source-only prompt/schema changed")
		}
		input := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(input, `"author_handle":"PublicAuthor"`) || !strings.Contains(input, `"post_relation":"quote"`) || strings.Contains(input, "internal-id") {
			t.Fatal("source metadata lost or leaked")
		}
		payload := `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"report_kind\":\"firsthand\",\"evidence_excerpt\":\"invented quote\",\"entity_relationship\":\"affected\"}"}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	original := Original{Text: "I cannot save.", SourceAuthor: "PublicAuthor", SourceRelation: "quote", SourceID: "internal-id"}
	f, err := Classify(NewClaudeClassifier("stub-key", "", client), original, "brand")
	if err != nil || f.ReportKind != "unknown" || f.EvidenceExcerpt != "" || calls != 1 {
		t.Fatalf("quote validation changed: %+v %v", f, err)
	}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.anthropic.com" {
			t.Fatal("fallback")
		}
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("stub-key private source"))}, nil
	})
	_, version, err := ForPersistence(NewClaudeClassifier("stub-key", "", client), original, "brand", nil)
	if err == nil || version != 0 || calls != 2 || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "stub-key") {
		t.Fatal("Claude failure not retryable or leaked")
	}
}
