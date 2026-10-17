package intelligence

import (
	"cleanapp-common/anthropic"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClaudeIntelligenceHasNoPaidFallback(t *testing.T) {
	provider := NewProvider("claude", "stub-key", "")
	c, ok := provider.(*ClaudeClient)
	if !ok || !c.Enabled() {
		t.Fatal("Claude not selected")
	}
	calls := 0
	c.api = anthropic.NewClient("stub-key", "", &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.anthropic.com" || r.URL.RawQuery != "" || r.Header.Get("x-api-key") != "stub-key" {
			t.Fatal("provider/key fallback")
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("bad request")
		}
		if b["max_tokens"] != float64(1200) || b["model"] != anthropic.DefaultModel || b["system"] != "Keep grounded" {
			t.Fatal("request contract")
		}
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("stub-key private source"))}, nil
	})})
	for _, mode := range []string{"fast", "deep"} {
		_, err := c.GenerateAnswerWithQuality(context.Background(), "Keep grounded", "original", mode)
		if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "stub-key") {
			t.Fatal("unsafe error")
		}
	}
	if calls != 2 {
		t.Fatal("Claude retried/fell back after auth failure")
	}
	if NewProvider("typo", "unused", "") != nil {
		t.Fatal("unknown provider fell back")
	}
	if _, ok := NewProvider("", "stub-key", "gemini-model").(*Client); !ok {
		t.Fatal("legacy default changed")
	}
}
func TestGeminiCredentialHeaderAndErrorSanitization(t *testing.T) {
	c := NewClient("stub-key", "model")
	c.httpClient = &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		if r.URL.RawQuery != "" || r.Header.Get("x-goog-api-key") != "stub-key" {
			t.Fatal("Gemini key in URL or missingheader")
		}
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"private source stub-key"}}`))}, nil
	})}
	_, err := c.GenerateAnswer(context.Background(), "private source", "report")
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "stub-key") {
		t.Fatal("provider body/key leaked")
	}
	c.httpClient.Transport = trip(func(r *http.Request) (*http.Response, error) { return nil, fmt.Errorf("private source stub-key") })
	_, err = c.call(context.Background(), "https://generativelanguage.googleapis.com/v1/models/model:generateContent", []byte(`{}`))
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "stub-key") {
		t.Fatal("transport error leaked")
	}
}
