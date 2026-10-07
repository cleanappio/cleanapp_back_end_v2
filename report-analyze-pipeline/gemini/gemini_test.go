package gemini

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerateContentFallbackAndSafeDiagnostics(t *testing.T) {
	var logs bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldOutput)
	c := NewClient("test-credential", "configured-model")
	calls := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.RawQuery != "" || r.Header.Get("x-goog-api-key") != "test-credential" {
			t.Fatal("credential must be in header only")
		}
		if !strings.Contains(r.URL.Path, "configured-model") {
			t.Fatal("configured model changed")
		}
		if calls == 1 {
			return nil, errors.New("request deadline exceeded")
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"status":"NOT_FOUND","message":"private-report-content"}}`)), Header: make(http.Header)}, nil
	})
	_, err := c.generateContent(geminiRequest{})
	if calls != 2 || err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("unexpected fallback: calls=%d err=%v", calls, err)
	}
	diagnostic := logs.String() + err.Error()
	for _, private := range []string{"test-credential", "private-report-content"} {
		if strings.Contains(diagnostic, private) {
			t.Fatal("private data appeared in diagnostics")
		}
	}
	for _, expected := range []string{"api_version=v1beta", "api_version=v1 ", "duration_ms=", "outcome=transport_error", "status=404"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("missing diagnostic %s", expected)
		}
	}
}

func TestGenerateContentSuccess(t *testing.T) {
	c := NewClient("test-credential", "configured-model")
	calls := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"model-result"}]}}]}`)), Header: make(http.Header)}, nil
	})
	result, err := c.generateContent(geminiRequest{})
	if result != "model-result" || err != nil || calls != 1 {
		t.Fatalf("result=%q err=%v calls=%d", result, err, calls)
	}
}
