// Package anthropic provides the first-party Claude Messages API transport.
// It never logs submitted content, provider response bodies, or credentials.
package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultModel         = "claude-haiku-5-5"
	MaxTokens            = 4096
	requestTimeout       = 90 * time.Second
	maxResponseBytes     = 1 << 20
	maxEncodedImageBytes = 10_000_000
	messagesURL          = "https://api.anthropic.com/v1/messages"
)

type Input struct {
	System     string
	Text       string
	Image      []byte
	JSONSchema map[string]any
	MaxTokens  int
}

type Client struct {
	key, model string
	http       *http.Client
}

func NewClient(key, model string, client *http.Client) *Client {
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	// Do not forward the API key to a redirected host. Copy rather than mutate
	// the caller's client, which may also serve other providers.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{key: strings.TrimSpace(key), model: strings.TrimSpace(model), http: &bounded}
}

func (c *Client) Enabled() bool { return c != nil && c.key != "" }

// Generate has one total deadline, at most two HTTP attempts, and no provider
// or model fallback. Only explicit rate-limit/server responses are retried.
func (c *Client) Generate(ctx context.Context, in Input) (string, error) {
	if !c.Enabled() {
		return "", errors.New("ANTHROPIC_API_KEY is required")
	}
	if in.MaxTokens == 0 {
		in.MaxTokens = MaxTokens
	}
	if in.MaxTokens < 1 || in.MaxTokens > MaxTokens {
		return "", errors.New("Claude max_tokens must be between 1 and 4096")
	}
	content := []any{}
	if strings.TrimSpace(in.Text) != "" {
		content = append(content, map[string]any{"type": "text", "text": in.Text})
	}
	if len(in.Image) > 0 {
		// The first-party API accepts at most 10 MB of base64 image data.
		if base64.StdEncoding.EncodedLen(len(in.Image)) > maxEncodedImageBytes {
			return "", errors.New("Claude image exceeded encoded size limit")
		}
		mime := http.DetectContentType(in.Image)
		switch mime {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return "", errors.New("unsupported Claude image format")
		}
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": base64.StdEncoding.EncodeToString(in.Image)}})
	}
	if len(content) == 0 {
		return "", errors.New("Claude requires report text or an image")
	}
	body := map[string]any{"model": c.model, "max_tokens": in.MaxTokens, "messages": []any{map[string]any{"role": "user", "content": content}}}
	if in.System != "" {
		body["system"] = in.System
	}
	// Haiku 5.5's default thinking shares max_tokens with the answer. Keep
	// these bounded extraction calls text-only; other configured models may
	// reject disabled thinking, so do not infer their capabilities.
	output := map[string]any{}
	if c.model == DefaultModel {
		body["thinking"] = map[string]any{"type": "disabled"}
		output["effort"] = "low"
	}
	// Haiku 5.5 rejects sampling controls; omit temperature/top_p/top_k.
	if in.JSONSchema != nil {
		output["format"] = map[string]any{"type": "json_schema", "schema": in.JSONSchema}
	}
	if len(output) > 0 {
		body["output_config"] = output
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", errors.New("Claude request could not be encoded")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("x-api-key", c.key)
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", errors.New("Claude transport failed")
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		_ = resp.Body.Close()
		if readErr != nil || len(raw) > maxResponseBytes {
			return "", errors.New("Claude response exceeded limit or could not be read")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if attempt == 0 && (resp.StatusCode == 429 || resp.StatusCode >= 500) {
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return "", fmt.Errorf("Claude API returned HTTP %d", resp.StatusCode)
		}
		var message struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil {
			return "", errors.New("Claude response was invalid")
		}
		if message.StopReason != "end_turn" && message.StopReason != "stop_sequence" {
			return "", errors.New("Claude response was incomplete or refused")
		}
		var text strings.Builder
		for _, block := range message.Content {
			if block.Type == "refusal" {
				return "", errors.New("Claude response was refused")
			}
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
		answer := strings.TrimSpace(text.String())
		if answer == "" {
			return "", errors.New("Claude returned no text")
		}
		if in.JSONSchema != nil {
			var object map[string]json.RawMessage
			if json.Unmarshal([]byte(answer), &object) != nil || object == nil {
				return "", errors.New("Claude returned invalid JSON object")
			}
		}
		return answer, nil
	}
	return "", errors.New("Claude request failed")
}
