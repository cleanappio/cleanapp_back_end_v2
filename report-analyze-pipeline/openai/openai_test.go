package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Capture the real AnalyzeImage request: checking a standalone prompt constant
// would miss a provider path that still sends the old prompt or drops context.
func TestAnalyzeImageComplaintSubjectContract(t *testing.T) {
	for _, tc := range []struct {
		name        string
		image       []byte
		description string
	}{
		{
			name:        "text grouping examples",
			description: "On CleanApp itself, I can't easily cluster multiple properties or sites into one brand cluster, eg clustering Grok and Grokipedia and XAI and X and SpaceX",
		},
		{
			name:        "image with erroneous brand label",
			image:       []byte("test-image-bytes"),
			description: "Problem with CleanApp brand attribution logic. The attached screenshot says Grok, but it's not ascribing to CleanApp itself for some reason.",
		},
		{
			name:        "reporting channel is not affected subject",
			description: "I used CleanApp to report a Grok login failure.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient("test-credential", "configured-model")
			calls := 0
			c.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var request struct {
					Model    string `json:"model"`
					Messages []struct {
						Role    string `json:"role"`
						Content []struct {
							Type     string   `json:"type"`
							Text     string   `json:"text"`
							ImageURL ImageURL `json:"image_url"`
						} `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Model != "configured-model" || len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
					t.Fatal("analysis request lost its configured model or role boundaries")
				}
				if len(request.Messages[0].Content) != 1 {
					t.Fatal("expected one system prompt")
				}
				prompt := request.Messages[0].Content[0].Text
				assertComplaintSubjectContract(t, prompt)
				if (len(tc.image) == 0) != strings.Contains(prompt, "You are NOT looking at an image unless one is actually attached") {
					t.Fatal("analysis request selected the wrong evidence mode")
				}
				userParts := request.Messages[1].Content
				wantParts := 1
				if len(tc.image) > 0 {
					wantParts++
				}
				if len(userParts) != wantParts {
					t.Fatal("analysis request lost user evidence")
				}
				if len(tc.image) > 0 {
					if userParts[0].Type != "image_url" || userParts[0].ImageURL.URL != encodeImageToBase64(tc.image) {
						t.Fatal("image evidence was changed or omitted")
					}
				}
				if userParts[len(userParts)-1].Type != "text" || userParts[len(userParts)-1].Text != tc.description {
					t.Fatal("original complaint must remain separate, unchanged user evidence")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"model-result"}}]}`)), Header: make(http.Header)}, nil
			})
			result, err := c.AnalyzeImage(tc.image, tc.description)
			if err != nil || result != "model-result" || calls != 1 {
				t.Fatalf("analysis request failed: result=%q calls=%d err=%v", result, calls, err)
			}
		})
	}
}

func assertComplaintSubjectContract(t *testing.T, prompt string) {
	t.Helper()
	for name, required := range map[string]string{
		"affected subject":       "the subject whose own functionality or site is reported to be failing",
		"incidental mentions":    "examples, comparisons, grouping/filter targets",
		"no reporting default":   "CleanApp is not a default brand",
		"CleanApp subject case":  `has brand_name "CleanApp"`,
		"Grok subject case":      `has brand_name "Grok"`,
		"product distinction":    "Grokipedia is not Grok",
		"erroneous image label":  "instead of repeating the erroneous screenshot label",
		"unknown is JSON null":   `JSON null (not the string "null")`,
		"aligned responsibility": "Keep responsible_party and inferred_contact_emails aligned with the affected subject",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("outgoing analysis prompt lacks %s rule", name)
		}
	}
	for _, obsolete := range []string{
		"Brand / platform detection — Use any of:",
		"<A brand name, if present, otherwise null>",
		"<product/service/organization discussed in the text, otherwise null>",
	} {
		if strings.Contains(prompt, obsolete) {
			t.Error("outgoing prompt still permits brand attribution from any mention")
		}
	}
}
