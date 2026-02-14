package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the email service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// SendGrid configuration
	SendGridAPIKey    string
	SendGridFromName  string
	SendGridFromEmail string

	// Service configuration
	OptOutURL    string
	PollInterval string
	HTTPPort     string

	// Email throttling configuration
	ThrottleDays int // Days to throttle emails per brand+email pair (default: 7)

	// Spam prevention configuration
	DryRun                 bool // If true, log emails but don't actually send them
	MaxDailyEmailsPerBrand int  // Maximum emails to send per brand per day (default: 10)

	// Physical contact discovery flow:
	// - When a physical report has no recipients yet (no inferred emails + no area emails),
	//   we can defer processing for a short period to allow background contact discovery
	//   (OSM/web enrichment, email-fetcher, etc) to populate inferred_contact_emails.
	UseInferredEmailsForPhysical bool // If true, allow sending to inferred_contact_emails for physical reports
	ContactDiscoveryMaxRetries   int  // Max defer/retry attempts before marking as processed (default: 3)
	ContactDiscoveryRetryMinutes int  // Base retry delay in minutes (exponential backoff) (default: 30)

	// Brandless physical pipeline:
	// Some physical reports have no brand name but still have location-based recipients.
	// This pipeline processes those seqs separately (bounded + configurable) so they don't get
	// dropped on the floor by aggregate brand grouping.
	BrandlessPhysicalEnabled      bool
	BrandlessPhysicalPollInterval string // duration string (default: 1m)
	BrandlessPhysicalBatchLimit   int    // max reports per tick (default: 5)
}

// Load loads configuration from environment variables and flags
func Load() *Config {
	cfg := &Config{}

	// Database configuration
	cfg.DBHost = getEnv("DB_HOST", "localhost")
	cfg.DBPort = getEnv("DB_PORT", "3306")
	cfg.DBUser = getEnv("DB_USER", "server")
	cfg.DBPassword = getEnv("DB_PASSWORD", "secret")
	cfg.DBName = getEnv("DB_NAME", "cleanapp")

	// SendGrid configuration
	cfg.SendGridAPIKey = getEnv("SENDGRID_API_KEY", "")
	cfg.SendGridFromName = getEnv("SENDGRID_FROM_NAME", "CleanApp")
	cfg.SendGridFromEmail = getEnv("SENDGRID_FROM_EMAIL", "info@cleanapp.io")

	// Service configuration
	cfg.OptOutURL = getEnv("OPT_OUT_URL", "http://localhost:8080/opt-out")
	cfg.PollInterval = getEnv("POLL_INTERVAL", "10s")
	cfg.HTTPPort = getEnv("HTTP_PORT", "8080")

	// Email throttling configuration
	throttleDays, err := strconv.Atoi(getEnv("EMAIL_THROTTLE_DAYS", "7"))
	if err != nil || throttleDays <= 0 {
		throttleDays = 7 // Default to 7 days
	}
	cfg.ThrottleDays = throttleDays

	// Spam prevention configuration
	cfg.DryRun = getEnv("EMAIL_DRY_RUN", "false") == "true"
	maxDaily, err := strconv.Atoi(getEnv("MAX_DAILY_EMAILS_PER_BRAND", "10"))
	if err != nil || maxDaily <= 0 {
		maxDaily = 10 // Default: max 10 emails per brand per day
	}
	cfg.MaxDailyEmailsPerBrand = maxDaily

	// Physical contact discovery flow defaults
	cfg.UseInferredEmailsForPhysical = getEnv("EMAIL_USE_INFERRED_EMAILS_FOR_PHYSICAL", "true") == "true"

	maxRetries, err := strconv.Atoi(getEnv("EMAIL_CONTACT_DISCOVERY_MAX_RETRIES", "3"))
	if err != nil || maxRetries < 0 {
		maxRetries = 3
	}
	cfg.ContactDiscoveryMaxRetries = maxRetries

	retryMinutes, err := strconv.Atoi(getEnv("EMAIL_CONTACT_DISCOVERY_RETRY_MINUTES", "30"))
	if err != nil || retryMinutes <= 0 {
		retryMinutes = 30
	}
	cfg.ContactDiscoveryRetryMinutes = retryMinutes

	// Brandless physical pipeline defaults
	cfg.BrandlessPhysicalEnabled = getEnv("EMAIL_BRANDLESS_PHYSICAL_ENABLED", "true") == "true"
	cfg.BrandlessPhysicalPollInterval = getEnv("EMAIL_BRANDLESS_PHYSICAL_POLL_INTERVAL", "1m")
	batchLimit, err := strconv.Atoi(getEnv("EMAIL_BRANDLESS_PHYSICAL_BATCH_LIMIT", "5"))
	if err != nil || batchLimit <= 0 {
		batchLimit = 5
	}
	cfg.BrandlessPhysicalBatchLimit = batchLimit

	return cfg
}

// GetPollInterval returns the parsed poll interval duration
func (c *Config) GetPollInterval() time.Duration {
	duration, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		// Fallback to default 30 seconds if parsing fails
		return 30 * time.Second
	}
	return duration
}

func (c *Config) GetBrandlessPhysicalPollInterval() time.Duration {
	duration, err := time.ParseDuration(c.BrandlessPhysicalPollInterval)
	if err != nil {
		return 1 * time.Minute
	}
	if duration <= 0 {
		return 1 * time.Minute
	}
	return duration
}

// GetHTTPPort returns the HTTP port as an integer
func (c *Config) GetHTTPPort() int {
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil {
		// Fallback to default port 8080 if parsing fails
		return 8080
	}
	return port
}

// getEnv gets an environment variable with a fallback default value
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
