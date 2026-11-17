package config

import (
	"testing"

	"cleanapp-common/mailreply"
)

func TestReplySecretDefaultsToTheOneEmailServiceDerives(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_PASSWORD", "test-only-password")
	t.Setenv("JWT_SECRET", "stub-jwt")
	t.Setenv("AMQP_USER", "stub")
	t.Setenv("AMQP_PASSWORD", "stub")
	t.Setenv("INTELLIGENCE_LLM_PROVIDER", "")
	t.Setenv("REPLY_TOKEN_SECRET", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReplyTokenSecret != string(mailreply.DeriveSecret("test-only-password")) || len(cfg.ReplyTokenSecret) < 16 {
		t.Fatal("the link secret must be the one derived from the database password")
	}
	if cfg.InboxEnabled || cfg.EscalationEnabled {
		t.Fatal("the inbox and silence workers stay off by default: they need mailbox access")
	}
	t.Setenv("REPLY_TOKEN_SECRET", "an-explicit-secret-of-enough-length")
	if cfg, _ = Load(); cfg.ReplyTokenSecret != "an-explicit-secret-of-enough-length" {
		t.Fatal("an explicit secret must win over the derived one")
	}
}
