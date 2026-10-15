package config

import (
	"testing"
	"time"
)

func TestEmailProviderConfig(t *testing.T) {
	t.Setenv("DB_PASSWORD", "test-only")
	t.Setenv("EMAIL_PROVIDER", "google_workspace")
	t.Setenv("SMTP_HOST", "smtp-relay.gmail.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USERNAME", "")
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("SMTP_PASSWORD_FILE", "")
	t.Setenv("SMTP_TIMEOUT", "30s")
	t.Setenv("EMAIL_FROM_NAME", "CleanApp Workspace")
	t.Setenv("EMAIL_FROM_ADDRESS", "info@cleanapp.io")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailProvider != "google_workspace" || cfg.EmailFromName != "CleanApp Workspace" || cfg.EmailFromAddress != "info@cleanapp.io" || cfg.SMTP.Host != "smtp-relay.gmail.com" || cfg.SMTP.Port != 587 {
		t.Fatal("Workspace provider configuration was not loaded")
	}
}

func TestLegacyEmailConfigStillWorks(t *testing.T) {
	t.Setenv("DB_PASSWORD", "test-only")
	t.Setenv("EMAIL_PROVIDER", "")
	t.Setenv("EMAIL_FROM_NAME", "")
	t.Setenv("EMAIL_FROM_ADDRESS", "")
	t.Setenv("SENDGRID_FROM_NAME", "Legacy CleanApp")
	t.Setenv("SENDGRID_FROM_EMAIL", "info@cleanapp.io")
	// SMTP configuration must not interfere with explicit/default SendGrid rollback.
	t.Setenv("SMTP_PORT", "invalid")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailProvider != "sendgrid" || cfg.EmailFromName != "Legacy CleanApp" || cfg.EmailFromAddress != "info@cleanapp.io" {
		t.Fatal("legacy email configuration fallback changed")
	}
}

func TestEmailProviderRejectsUnknownProvider(t *testing.T) {
	t.Setenv("DB_PASSWORD", "test-only")
	t.Setenv("EMAIL_PROVIDER", "unsupported")
	if _, err := Load(); err == nil {
		t.Fatal("unsupported email provider accepted")
	}
}

func TestCompanyNotificationIntervalIsIndependentOfLegacyDayThrottle(t *testing.T) {
	t.Setenv("DB_PASSWORD", "test-only")
	t.Setenv("EMAIL_PROVIDER", "sendgrid")
	t.Setenv("EMAIL_THROTTLE_DAYS", "17")
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 5 * time.Minute}, {"invalid", 5 * time.Minute}, {"0", 5 * time.Minute}, {"-1m", 5 * time.Minute}, {"90s", 90 * time.Second}, {"7m", 7 * time.Minute},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("COMPANY_NOTIFICATION_INTERVAL", tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GetCompanyNotificationInterval() != tc.want || cfg.ThrottleDays != 17 {
				t.Fatalf("company cadence leaked into legacy throttle: interval=%v days=%d", cfg.GetCompanyNotificationInterval(), cfg.ThrottleDays)
			}
		})
	}
	if (&Config{}).GetCompanyNotificationInterval() != 5*time.Minute {
		t.Fatal("programmatic configurations lost the safe default")
	}
}
