package service

import (
	"cleanapp-common/evidence"
	"report-analyze-pipeline/config"
	"testing"
)

func TestSelectedClaudeAlsoClassifiesOriginalEvidence(t *testing.T) {
	c, err := newLLMClient(&config.Config{LLMProvider: "claude", AnthropicAPIKey: "stub-key", GeminiAPIKey: "unused", OpenAIAPIKey: "unused"})
	if err != nil || c.SourceName() != "Claude" {
		t.Fatalf("wrong provider %v", err)
	}
	if _, ok := c.(evidence.OriginalClassifier); !ok {
		t.Fatal("Claude evidence path missing original context")
	}
	if _, err := newLLMClient(&config.Config{LLMProvider: "claude", GeminiAPIKey: "unused", OpenAIAPIKey: "unused"}); err == nil {
		t.Fatal("missing Claude key fell back")
	}
	if _, err := newLLMClient(&config.Config{LLMProvider: "typo", OpenAIAPIKey: "unused"}); err == nil {
		t.Fatal("invalid provider fell back")
	}
}
