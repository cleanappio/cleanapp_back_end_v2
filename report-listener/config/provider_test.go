package config

import (
	"cleanapp-common/anthropic"
	"strings"
	"testing"
)

func TestClaudeIntelligenceConfigIsExplicitAndFailClosed(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_PASSWORD", "stub")
	t.Setenv("JWT_SECRET", "stub-jwt")
	t.Setenv("AMQP_USER", "stub")
	t.Setenv("AMQP_PASSWORD", "stub")
	t.Setenv("INTELLIGENCE_LLM_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	c, err := Load()
	if err != nil || c.IntelligenceLLMProvider != "gemini" {
		t.Fatalf("legacy default changed %v", err)
	}
	t.Setenv("INTELLIGENCE_LLM_PROVIDER", "claude")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatal("missing Claude key fell back")
	}
	t.Setenv("ANTHROPIC_API_KEY", "stub-key")
	c, err = Load()
	if err != nil || c.AnthropicModel != anthropic.DefaultModel {
		t.Fatalf("Claude config %v", err)
	}
	t.Setenv("INTELLIGENCE_LLM_PROVIDER", "private-invalid-provider")
	if _, err := Load(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("unknown provider fell back or echoed config")
	}
}
