package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: strings.TrimSpace(apiKey),
		model:  strings.TrimSpace(model),
		httpClient: &http.Client{
			Timeout: 18 * time.Second,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

type geminiRequest struct {
	SystemInstruction *geminiSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []geminiContent          `json:"contents"`
	GenerationConfig  geminiGenerationConfig   `json:"generationConfig"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) GenerateAnswer(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return c.GenerateAnswerWithQuality(ctx, systemPrompt, userPrompt, "")
}

func (c *Client) GenerateAnswerWithQuality(ctx context.Context, systemPrompt, userPrompt, qualityMode string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("gemini api key not configured")
	}

	models := c.modelsForQualityMode(qualityMode)

	reqBody := geminiRequest{
		SystemInstruction: &geminiSystemInstruction{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: userPrompt}},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			Temperature:     0.25,
			MaxOutputTokens: 1200,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	var lastErr error
	for _, model := range models {
		endpoints := []string{
			fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, c.apiKey),
			fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent?key=%s", model, c.apiKey),
		}
		for _, url := range endpoints {
			answer, callErr := c.call(ctx, url, payload)
			if callErr == nil && strings.TrimSpace(answer) != "" {
				return strings.TrimSpace(answer), nil
			}
			lastErr = callErr
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("gemini returned empty response")
	}
	return "", lastErr
}

func (c *Client) modelsForQualityMode(mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "fast":
		return dedupeNonEmpty(
			"gemini-2.5-flash",
			"gemini-2.0-flash",
			"gemini-flash-latest",
			c.model,
			"gemini-2.5-pro",
		)
	case "deep":
		fallthrough
	default:
		return dedupeNonEmpty(
			c.model,
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gemini-2.5-flash-lite",
			"gemini-2.0-flash",
			"gemini-flash-latest",
		)
	}
}

func (c *Client) call(ctx context.Context, url string, payload []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var parsed geminiResponse
	_ = json.Unmarshal(body, &parsed)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("gemini http %d: %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("gemini http %d", resp.StatusCode)
	}

	if len(parsed.Candidates) == 0 {
		return "", fmt.Errorf("gemini returned no candidates")
	}

	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(p.Text))
	}
	return b.String(), nil
}

func dedupeNonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		val := strings.TrimSpace(v)
		if val == "" {
			continue
		}
		if _, ok := seen[val]; ok {
			continue
		}
		seen[val] = struct{}{}
		out = append(out, val)
	}
	return out
}
