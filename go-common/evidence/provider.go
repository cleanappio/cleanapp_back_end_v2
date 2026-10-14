package evidence

import (
	"bytes"
	"cleanapp-common/appenv"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"time"
)

// All evidence-only calls in this process share a budget, including the main
// analyzer and the import worker. Full analysis keeps its existing queue limits.
var requestLimiter = rate.NewLimiter(rate.Every(evidenceRequestInterval()), 1)

func evidenceRequestInterval() time.Duration {
	requests := appenv.Int("EVIDENCE_REQUESTS_PER_MINUTE", 20)
	if requests < 1 || requests > 120 {
		requests = 20
	}
	return time.Minute / time.Duration(requests)
}

// Provider uses one shared schema for production and dry-run/backfill. It has no
// routing or database side effects, and never logs evidence or credentials.
type Provider struct {
	kind, key, model string
	http             *http.Client
}

func NewOpenAIClassifier(key, model string, client *http.Client) *Provider {
	return newProvider("openai", key, model, client)
}
func NewGeminiClassifier(key, model string, client *http.Client) *Provider {
	return newProvider("gemini", key, model, client)
}
func newProvider(kind, key, model string, client *http.Client) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &Provider{kind: kind, key: key, model: model, http: client}
}
func (p *Provider) AnalyzeEvidence(original, entity string) (Fields, error) {
	return p.AnalyzeEvidenceImage(nil, original, entity)
}
func (p *Provider) AnalyzeEvidenceImage(image []byte, original, entity string) (Fields, error) {
	prompt := Prompt
	mimeType := http.DetectContentType(image)
	hasImage := ValidOriginalImage(image)
	if hasImage {
		prompt += "\n" + ImageInstructions
	}
	var request any
	var endpoints []string
	if p.kind == "openai" {
		content := []any{map[string]any{"type": "text", "text": Input(original, entity)}}
		if hasImage {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(image)}})
		}
		request = map[string]any{"model": p.model, "messages": []any{map[string]any{"role": "system", "content": prompt}, map[string]any{"role": "user", "content": content}}}
		endpoints = []string{"https://api.openai.com/v1/chat/completions"}
	} else {
		parts := []any{map[string]any{"text": prompt}, map[string]any{"text": Input(original, entity)}}
		if hasImage {
			parts = append(parts, map[string]any{"inline_data": map[string]any{"mime_type": mimeType, "data": base64.StdEncoding.EncodeToString(image)}})
		}
		request = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}}
		endpoints = []string{fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", p.model), fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", p.model)}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Fields{}, err
	}
	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return Fields{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		if p.kind == "openai" {
			req.Header.Set("Authorization", "Bearer "+p.key)
		} else {
			req.Header.Set("x-goog-api-key", p.key)
		}
		if err := requestLimiter.Wait(context.Background()); err != nil {
			return Fields{}, fmt.Errorf("evidence request budget unavailable")
		}
		resp, err := p.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("evidence provider transport failed")
			continue
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("evidence provider response could not be read")
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("evidence provider returned status %d", resp.StatusCode)
			continue
		}
		text, err := providerText(p.kind, payload)
		if err != nil {
			return Fields{}, err
		}
		return ParseWithMedia(text, original, hasImage)
	}
	return Fields{}, lastErr
}
func providerText(kind string, payload []byte) (string, error) {
	if kind == "openai" {
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(payload, &out) == nil && len(out.Choices) > 0 && out.Choices[0].Message.Content != "" {
			return out.Choices[0].Message.Content, nil
		}
	} else {
		var out struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal(payload, &out) == nil && len(out.Candidates) > 0 {
			for _, part := range out.Candidates[0].Content.Parts {
				if part.Text != "" {
					return part.Text, nil
				}
			}
		}
	}
	return "", fmt.Errorf("evidence provider returned no valid content")
}
