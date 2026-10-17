package intelligence

import (
	"cleanapp-common/anthropic"
	"context"
	"net/http"
	"time"
)

type AnswerClient interface {
	Enabled() bool
	GenerateAnswerWithQuality(context.Context, string, string, string) (string, error)
}

type ClaudeClient struct{ api *anthropic.Client }

func NewClaudeClient(key, model string) *ClaudeClient {
	return &ClaudeClient{api: anthropic.NewClient(key, model, &http.Client{Timeout: 18 * time.Second})}
}
func (c *ClaudeClient) Enabled() bool { return c != nil && c.api.Enabled() }
func (c *ClaudeClient) GenerateAnswerWithQuality(ctx context.Context, system, user, _ string) (string, error) {
	// Quality modes never select another provider or a more expensive model.
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	return c.api.Generate(ctx, anthropic.Input{System: system, Text: user, MaxTokens: 1200})
}

func NewProvider(provider, key, model string) AnswerClient {
	switch provider {
	case "claude":
		return NewClaudeClient(key, model)
	case "gemini", "":
		return NewClient(key, model)
	default:
		return nil
	}
}
