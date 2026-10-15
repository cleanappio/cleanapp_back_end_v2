package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Check the actual request in all analysis entry points, including the
// location-aware image path, rather than only inspecting unused prompt text.
func TestAnalyzeImageComplaintSubjectContract(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		image       []byte
		description string
		location    *LocationContext
	}{
		{
			name:        "text grouping examples",
			description: "On CleanApp itself, I can't easily cluster multiple properties or sites into one brand cluster, eg clustering Grok and Grokipedia and XAI and X and SpaceX",
		},
		{
			name:        "image with erroneous brand label",
			image:       imageBytes.Bytes(),
			description: "Problem with CleanApp brand attribution logic. The attached screenshot says Grok, but it's not ascribing to CleanApp itself for some reason.",
		},
		{
			name:        "reporting channel is not affected subject",
			description: "I used CleanApp to report a Grok login failure.",
		},
		{
			name:        "location-aware image",
			image:       imageBytes.Bytes(),
			description: "The entrance to Example Campus is blocked; the screenshot includes an unrelated Grok advertisement.",
			location:    &LocationContext{PrimaryName: "Example Campus", ParentOrg: "Example University", Domain: "example.edu"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient("test-credential", "configured-model")
			calls := 0
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var request geminiRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(r.URL.Path, "configured-model") || len(request.Contents) != 1 || request.Contents[0].Role != "user" {
					t.Fatal("analysis request lost its configured model or content boundary")
				}
				parts := request.Contents[0].Parts
				wantParts := 2
				if len(tc.image) > 0 {
					wantParts++
				}
				if len(parts) != wantParts {
					t.Fatal("analysis request lost prompt or user evidence")
				}
				assertComplaintSubjectContract(t, parts[0].Text)
				if (len(tc.image) == 0) != strings.Contains(parts[0].Text, "You are NOT looking at an image unless one is actually attached") {
					t.Fatal("analysis request selected the wrong evidence mode")
				}
				if parts[1].Text != tc.description {
					t.Fatal("original complaint must remain separate, unchanged evidence")
				}
				if len(tc.image) > 0 {
					attachment := parts[2].InlineData
					if attachment == nil || attachment.MimeType != "image/png" || attachment.Data != base64.StdEncoding.EncodeToString(tc.image) {
						t.Fatal("image evidence was changed or omitted")
					}
				}
				if tc.location != nil {
					for _, expected := range []string{"LOCATION_CONTEXT", "Example Campus", "Example University", "example.edu"} {
						if !strings.Contains(parts[0].Text, expected) {
							t.Fatal("location-aware analysis lost supplied attribution context")
						}
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"model-result"}]}}]}`)), Header: make(http.Header)}, nil
			})
			var result string
			var err error
			if tc.location != nil {
				result, err = c.AnalyzeImageWithLocation(tc.image, tc.description, tc.location)
			} else {
				result, err = c.AnalyzeImage(tc.image, tc.description)
			}
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
