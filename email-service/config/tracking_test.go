package config

import (
	"testing"

	"cleanapp-common/mailreply"
)

func TestTrackingIsOnByDefaultWithTheDerivedSecret(t *testing.T) {
	t.Setenv("DB_PASSWORD", "test-only-password")
	t.Setenv("EMAIL_PROVIDER", "")
	for _, key := range []string{"REPLY_TOKEN_SECRET", "REPLY_TRACKING_ENABLED", "VIEW_TRACKING_ENABLED"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReplyTrackingEnabled || !cfg.ViewTrackingEnabled {
		t.Fatalf("reply=%v view=%v: both must be on by default", cfg.ReplyTrackingEnabled, cfg.ViewTrackingEnabled)
	}
	// report-listener derives its secret the same way from the same password.
	if cfg.ReplyTokenSecret != string(mailreply.DeriveSecret("test-only-password")) || len(cfg.ReplyTokenSecret) < 16 {
		t.Fatal("the secret must be the one derived from the database password")
	}

	t.Setenv("REPLY_TOKEN_SECRET", "an-explicit-secret-of-enough-length")
	if cfg, _ = Load(); cfg.ReplyTokenSecret != "an-explicit-secret-of-enough-length" {
		t.Fatal("an explicit secret must win over the derived one")
	}
	t.Setenv("VIEW_TRACKING_ENABLED", "false")
	if cfg, _ = Load(); !cfg.ReplyTrackingEnabled || cfg.ViewTrackingEnabled {
		t.Fatal("view tracking must be switchable off by itself")
	}
	t.Setenv("VIEW_TRACKING_ENABLED", "")
	t.Setenv("REPLY_TRACKING_ENABLED", "false")
	if cfg, _ = Load(); cfg.ReplyTrackingEnabled || cfg.ViewTrackingEnabled {
		t.Fatal("turning reply tracking off must turn view tracking off too")
	}
}
