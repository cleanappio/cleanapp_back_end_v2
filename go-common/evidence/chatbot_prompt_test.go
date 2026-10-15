package evidence

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

// These are request/normalization contracts, not a simulated model evaluation.
// The provider fixtures ensure both providers and both evidence modes receive
// the observed-behavior distinction while retaining quote/ownership context.
func TestProvidersDistinguishObservedChatbotBehaviorFromRelayedClaims(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"openai", "gemini"} {
		for _, withImage := range []bool{false, true} {
			mode := "text"
			if withImage {
				mode = "image"
			}
			for _, tc := range []struct {
				name, original, relation, reportKind, excerpt string
			}{
				{
					name:       "observed failure without first-person pronoun",
					original:   "GrokBot / Grok has this weird habit of refusing something, and asking to do a given task in a 1:1, without ANY context of what that means, or where such a 1:1 might exist. A simple link would suffice.",
					reportKind: "firsthand",
					excerpt:    "GrokBot / Grok has this weird habit of refusing something, and asking to do a given task in a 1:1, without ANY context of what that means, or where such a 1:1 might exist.",
				},
				{
					name:       "another user's incident and bot acknowledgement",
					original:   "Another user posted that their Grok export crashes. The bot replied that it was investigating their bug; forwarding their screenshot.",
					relation:   "quote",
					reportKind: "coverage",
					excerpt:    "Another user posted that their Grok export crashes.",
				},
				{
					name:       "bot patch and bounty status without observed failure",
					original:   "Our team is reviewing bounty logistics and has fully patched the glitch. DM sent.",
					reportKind: "unknown",
				},
				{
					name:       "generic apology without concrete problem",
					original:   "Grok replied: Sorry about that, we are checking.",
					reportKind: "unknown",
				},
				{
					name:       "ambiguous conversation ownership",
					original:   "A Grok conversation screenshot, with no indication who encountered a problem.",
					reportKind: "unknown",
				},
			} {
				t.Run(kind+"/"+mode+"/"+tc.name, func(t *testing.T) {
					source := Original{Text: tc.original, SourceRelation: tc.relation, SourceAuthor: "source-author"}
					if withImage {
						source.Image = raw.Bytes()
					}
					calls := 0
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						var body map[string]any
						if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						var prompt, input, media string
						if kind == "openai" {
							messages := body["messages"].([]any)
							prompt = messages[0].(map[string]any)["content"].(string)
							parts := messages[1].(map[string]any)["content"].([]any)
							input = parts[0].(map[string]any)["text"].(string)
							if withImage {
								if len(parts) != 2 {
									t.Fatal("original image omitted")
								}
								media = parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
							} else if len(parts) != 1 {
								t.Fatal("text-only source acquired invented media")
							}
						} else {
							parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
							prompt = parts[0].(map[string]any)["text"].(string)
							input = parts[1].(map[string]any)["text"].(string)
							if withImage {
								if len(parts) != 3 {
									t.Fatal("original image omitted")
								}
								media = parts[2].(map[string]any)["inline_data"].(map[string]any)["data"].(string)
							} else if len(parts) != 2 {
								t.Fatal("text-only source acquired invented media")
							}
						}
						assertChatbotEvidenceContract(t, prompt, withImage)
						if input != OriginalInput(source, "Grok") {
							t.Fatal("original text, subject, or author/repost context changed")
						}
						if withImage && !strings.HasSuffix(media, base64.StdEncoding.EncodeToString(source.Image)) {
							t.Fatal("original image bytes changed")
						}
						fields, err := json.Marshal(Fields{ReportKind: tc.reportKind, EvidenceExcerpt: tc.excerpt, EntityRelationship: "affected", ClassificationReason: "Fixture based on retained source."})
						if err != nil {
							t.Fatal(err)
						}
						payload := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(fields)}}}}
						if kind == "gemini" {
							payload = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": string(fields)}}}}}}
						}
						encoded, _ := json.Marshal(payload)
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(encoded)), Header: make(http.Header)}, nil
					})}
					provider := NewOpenAIClassifier("test-secret", "configured-model", client)
					if kind == "gemini" {
						provider = NewGeminiClassifier("test-secret", "configured-model", client)
					}
					got, err := Classify(provider, source, "Grok")
					if err != nil || calls != 1 || got.ReportKind != tc.reportKind || got.EvidenceExcerpt != tc.excerpt || got.ReportedImpact != "" {
						t.Fatalf("classification changed evidence or overrode uncertainty: kind=%q quote=%q calls=%d err=%v", got.ReportKind, got.EvidenceExcerpt, calls, err)
					}
				})
			}
		}
	}
}

func assertChatbotEvidenceContract(t *testing.T, prompt string, withImage bool) {
	t.Helper()
	for name, required := range map[string]string{
		"observation without pronouns":  "Personal pronouns such as \"I\" are not required",
		"observed bot behavior":         "A chatbot response can be the observed malfunction",
		"no blanket bot downgrade":      "mere presence of a chatbot conversation does not make the report coverage or unknown",
		"native intake is insufficient": "Native submission, an attached conversation, or a bot's first-person wording alone does not establish the author's experience",
		"relayed claims":                "another user's concrete incident without this author's own observation, classify coverage",
		"status uncertainty":            "if they merely coordinate or assert status, classify unknown",
		"repost ownership":              "A quote/repost may be firsthand only if the current author's own wording separately describes their own encountered problem",
		"literal excerpt":               "The excerpt must be copied exactly",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("outgoing prompt lacks %s rule", name)
		}
	}
	if withImage {
		for _, required := range []string{
			"the confusing chatbot instruction itself is the reported user-experience failure",
			"Personal pronouns and visible personal identifiers are not required",
			"if the author-observation connection is unsupported or ambiguous, use unknown",
			"Never invent a transcription or a quotation from the image",
		} {
			if !strings.Contains(prompt, required) {
				t.Error("image prompt lost observed-behavior or ownership safeguards")
			}
		}
	} else if strings.Contains(prompt, "An original source image is attached") {
		t.Error("text-only source must not claim an image was attached")
	}
	if strings.Contains(prompt, "Screenshots of journalism, chatbot conversations, or another person's report are coverage when relayed") {
		t.Error("blanket chatbot-conversation downgrade remains in the request")
	}
}
