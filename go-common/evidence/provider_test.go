package evidence

import (
	"bytes"
	"encoding/json"
	"golang.org/x/time/rate"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProvidersUseSameSourceOnlySchemaAndValidateQuotes(t *testing.T) {
	for _, kind := range []string{"openai", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				if !strings.Contains(string(body), `Classify only the supplied ORIGINAL_TEXT`) || !strings.Contains(string(body), `I cannot save my session.`) || strings.Contains(req.URL.String(), "test-secret") {
					t.Fatal("invalid source-only request")
				}
				fields := `{"report_kind":"firsthand","evidence_excerpt":"invented quote"}`
				var payload any
				if kind == "openai" {
					payload = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": fields}}}}
				} else {
					payload = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": fields}}}}}}
				}
				encoded, _ := json.Marshal(payload)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}, nil
			})}
			provider := NewOpenAIClassifier("test-secret", "model", client)
			if kind == "gemini" {
				provider = NewGeminiClassifier("test-secret", "model", client)
			}
			result, err := provider.AnalyzeEvidence("I cannot save my session.", "Claude")
			if err != nil || result.ReportKind != "unknown" || result.EvidenceExcerpt != "" {
				t.Fatalf("got %#v %v", result, err)
			}
		})
	}
}

func TestProviderErrorsNeverEchoOriginalOrCredentials(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"private-source test-secret"}`)), Header: make(http.Header)}, nil
	})}
	_, err := NewOpenAIClassifier("test-secret", "model", client).AnalyzeEvidence("private-source", "brand")
	if err == nil || strings.Contains(err.Error(), "private-source") || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("unsafe provider error %v", err)
	}
}

func TestPhotoOnlyOriginalIsAttachedToBothProviders(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"openai", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				if !strings.Contains(string(body), "An original source image is attached.") {
					t.Fatal("missing image grounding instructions")
				}
				field := `{"report_kind":"firsthand","evidence_excerpt":"","classification_reason":"Original photo shows a blocked ramp.","entity_relationship":"affected"}`
				var payload any
				if kind == "openai" {
					if !strings.Contains(string(body), "image_url") {
						t.Fatal("no original image")
					}
					payload = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": field}}}}
				} else {
					if !strings.Contains(string(body), "inline_data") {
						t.Fatal("no original image")
					}
					payload = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": field}}}}}}
				}
				encoded, _ := json.Marshal(payload)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(encoded)), Header: make(http.Header)}, nil
			})}
			provider := NewOpenAIClassifier("test-secret", "model", client)
			if kind == "gemini" {
				provider = NewGeminiClassifier("test-secret", "model", client)
			}
			result, err := Classify(provider, Original{Image: raw.Bytes()}, "Property")
			if err != nil || result.ReportKind != "firsthand" || result.EvidenceExcerpt != "" {
				t.Fatalf("got %#v err=%v", result, err)
			}
		})
	}
}

func TestPlaceholderImageCannotEstablishEvidence(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if ValidOriginalImage(raw.Bytes()) || ValidOriginalImage([]byte("placeholder")) {
		t.Fatal("placeholder counted as original media")
	}
}

// Provider fixtures are in-memory HTTP transports; no real request budget is
// consumed by these tests.
func TestMain(m *testing.M) { requestLimiter = rate.NewLimiter(rate.Inf, 1); os.Exit(m.Run()) }

func TestEvidenceRequestBudgetConfigurationIsBounded(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{{"20", 3 * time.Second}, {"120", 500 * time.Millisecond}, {"0", 3 * time.Second}, {"1000", 3 * time.Second}} {
		t.Setenv("EVIDENCE_REQUESTS_PER_MINUTE", tt.value)
		if got := evidenceRequestInterval(); got != tt.want {
			t.Fatalf("%s: interval=%s want=%s", tt.value, got, tt.want)
		}
	}
}
