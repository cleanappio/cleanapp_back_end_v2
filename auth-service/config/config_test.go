package config

import (
	"strings"
	"testing"
)

func emailTestEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"APP_ENV": "test", "DB_PASSWORD": "test-only-database-password",
		"JWT_SECRET": "test-only-jwt-secret", "EMAIL_PROVIDER": "",
		"EMAIL_FROM_NAME": "", "EMAIL_FROM_ADDRESS": "", "SENDGRID_API_KEY": "",
		"SENDGRID_FROM_NAME": "", "SENDGRID_FROM_EMAIL": "",
		"SMTP_HOST": "smtp.gmail.com", "SMTP_PORT": "587",
		"SMTP_USERNAME": "info@cleanapp.io", "SMTP_PASSWORD": "", "SMTP_PASSWORD_FILE": "", "SMTP_TIMEOUT": "30s",
	} {
		t.Setenv(key, value)
	}
}

func TestEmailProviderDefaultsToLegacySendGrid(t *testing.T) {
	emailTestEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailProvider != "sendgrid" || cfg.EmailFromAddress != "info@cleanapp.io" || cfg.EmailFromName != "CleanApp" {
		t.Fatalf("unexpected default email configuration: provider=%q from=%q name=%q", cfg.EmailProvider, cfg.EmailFromAddress, cfg.EmailFromName)
	}
}

func TestEmailSenderIdentityUsesNewVariablesWithLegacyFallback(t *testing.T) {
	emailTestEnvironment(t)
	t.Setenv("SENDGRID_FROM_NAME", "Legacy Name")
	t.Setenv("SENDGRID_FROM_EMAIL", "legacy@example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailFromName != "Legacy Name" || cfg.EmailFromAddress != "legacy@example.com" {
		t.Fatal("legacy sender identity was not retained")
	}
	t.Setenv("EMAIL_FROM_NAME", "Workspace Name")
	t.Setenv("EMAIL_FROM_ADDRESS", "info@cleanapp.io")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailFromName != "Workspace Name" || cfg.EmailFromAddress != "info@cleanapp.io" {
		t.Fatal("new sender identity did not override legacy variables")
	}
}

func TestWorkspaceEmailConfigurationDoesNotRequireSendGrid(t *testing.T) {
	emailTestEnvironment(t)
	t.Setenv("EMAIL_PROVIDER", " Google_Workspace ")
	t.Setenv("SMTP_HOST", "smtp-relay.gmail.com")
	t.Setenv("SMTP_USERNAME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmailProvider != "google_workspace" || cfg.SendGridAPIKey != "" || cfg.SMTPConfig.Host != "smtp-relay.gmail.com" || cfg.SMTPConfig.Username != "" {
		t.Fatal("Workspace was not selected without SendGrid credentials")
	}
}

func TestSelectedWorkspaceEmailRejectsIncompleteAuthentication(t *testing.T) {
	emailTestEnvironment(t)
	t.Setenv("EMAIL_PROVIDER", "google_workspace")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "google_workspace") {
		t.Fatalf("expected Workspace configuration error, got %v", err)
	}
}

func TestEmailConfigurationRejectsUnknownProvider(t *testing.T) {
	emailTestEnvironment(t)
	t.Setenv("EMAIL_PROVIDER", "unsupported")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EMAIL_PROVIDER") {
		t.Fatalf("expected provider configuration error, got %v", err)
	}
}
