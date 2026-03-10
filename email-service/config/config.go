package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"cleanapp-common/appenv"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	SendGridAPIKey    string
	SendGridFromName  string
	SendGridFromEmail string

	OptOutURL    string
	PollInterval string
	HTTPPort     string

	ThrottleDays                  int
	DryRun                        bool
	MaxDailyEmailsPerBrand        int
	UseInferredEmailsForPhysical  bool
	ContactDiscoveryMaxRetries    int
	ContactDiscoveryRetryMinutes  int
	BrandlessPhysicalEnabled      bool
	BrandlessPhysicalPollInterval string
	BrandlessPhysicalBatchLimit   int
	NoValidEmailsBaseHours        int
	NoValidEmailsMaxBackoffHours  int
	NoValidEmailsMaxRetries       int

	AllowedOrigins     []string
	RateLimitRPS       float64
	RateLimitBurst     int
	InternalAdminToken string
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	cfg.DBHost = appenv.String("DB_HOST", "localhost")
	cfg.DBPort = appenv.String("DB_PORT", "3306")
	cfg.DBUser = appenv.String("DB_USER", "server")
	cfg.DBPassword = dbPassword
	cfg.DBName = appenv.String("DB_NAME", "cleanapp")
	cfg.SendGridAPIKey = appenv.String("SENDGRID_API_KEY", "")
	cfg.SendGridFromName = appenv.String("SENDGRID_FROM_NAME", "CleanApp")
	cfg.SendGridFromEmail = appenv.String("SENDGRID_FROM_EMAIL", "info@cleanapp.io")
	cfg.PollInterval = appenv.String("POLL_INTERVAL", "10s")
	cfg.HTTPPort = appenv.String("HTTP_PORT", "8080")
	cfg.OptOutURL = resolveOptOutURL(cfg.HTTPPort)
	cfg.AllowedOrigins = appenv.Strings("ALLOWED_ORIGINS")
	cfg.RateLimitRPS = float64(appenv.Int("RATE_LIMIT_RPS", 10))
	cfg.RateLimitBurst = appenv.Int("RATE_LIMIT_BURST", 20)
	cfg.InternalAdminToken = appenv.String("INTERNAL_ADMIN_TOKEN", "")

	throttleDays, err := strconv.Atoi(appenv.String("EMAIL_THROTTLE_DAYS", "7"))
	if err != nil || throttleDays <= 0 {
		throttleDays = 7
	}
	cfg.ThrottleDays = throttleDays
	cfg.DryRun = appenv.Bool("EMAIL_DRY_RUN", false)
	maxDaily, err := strconv.Atoi(appenv.String("MAX_DAILY_EMAILS_PER_BRAND", "10"))
	if err != nil || maxDaily <= 0 {
		maxDaily = 10
	}
	cfg.MaxDailyEmailsPerBrand = maxDaily
	cfg.UseInferredEmailsForPhysical = appenv.Bool("EMAIL_USE_INFERRED_EMAILS_FOR_PHYSICAL", true)
	maxRetries, err := strconv.Atoi(appenv.String("EMAIL_CONTACT_DISCOVERY_MAX_RETRIES", "3"))
	if err != nil || maxRetries < 0 {
		maxRetries = 3
	}
	cfg.ContactDiscoveryMaxRetries = maxRetries
	retryMinutes, err := strconv.Atoi(appenv.String("EMAIL_CONTACT_DISCOVERY_RETRY_MINUTES", "30"))
	if err != nil || retryMinutes <= 0 {
		retryMinutes = 30
	}
	cfg.ContactDiscoveryRetryMinutes = retryMinutes
	cfg.BrandlessPhysicalEnabled = appenv.Bool("EMAIL_BRANDLESS_PHYSICAL_ENABLED", true)
	cfg.BrandlessPhysicalPollInterval = appenv.String("EMAIL_BRANDLESS_PHYSICAL_POLL_INTERVAL", "1m")
	batchLimit, err := strconv.Atoi(appenv.String("EMAIL_BRANDLESS_PHYSICAL_BATCH_LIMIT", "5"))
	if err != nil || batchLimit <= 0 {
		batchLimit = 5
	}
	cfg.BrandlessPhysicalBatchLimit = batchLimit
	baseHours, err := strconv.Atoi(appenv.String("EMAIL_NO_VALID_EMAILS_BASE_HOURS", "6"))
	if err != nil || baseHours <= 0 {
		baseHours = 6
	}
	cfg.NoValidEmailsBaseHours = baseHours
	maxBackoffHours, err := strconv.Atoi(appenv.String("EMAIL_NO_VALID_EMAILS_MAX_BACKOFF_HOURS", "168"))
	if err != nil || maxBackoffHours <= 0 {
		maxBackoffHours = 168
	}
	cfg.NoValidEmailsMaxBackoffHours = maxBackoffHours
	maxRetriesNoValid, err := strconv.Atoi(appenv.String("EMAIL_NO_VALID_EMAILS_MAX_RETRIES", "10"))
	if err != nil || maxRetriesNoValid <= 0 {
		maxRetriesNoValid = 10
	}
	cfg.NoValidEmailsMaxRetries = maxRetriesNoValid
	return cfg, nil
}

func resolveOptOutURL(httpPort string) string {
	if value := strings.TrimSpace(appenv.String("OPT_OUT_URL", "")); value != "" {
		return value
	}
	if baseURL := strings.TrimRight(strings.TrimSpace(appenv.String("CLEANAPP_BASE_URL", "")), "/"); baseURL != "" {
		return fmt.Sprintf("%s/opt-out", baseURL)
	}
	if frontendURL := strings.TrimRight(strings.TrimSpace(appenv.String("FRONTEND_URL", "")), "/"); frontendURL != "" {
		return fmt.Sprintf("%s/opt-out", frontendURL)
	}
	if appenv.IsDevLike() {
		return fmt.Sprintf("http://localhost:%s/opt-out", httpPort)
	}
	return "https://cleanapp.io/opt-out"
}

func (c *Config) GetPollInterval() time.Duration {
	duration, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return 30 * time.Second
	}
	return duration
}

func (c *Config) GetBrandlessPhysicalPollInterval() time.Duration {
	duration, err := time.ParseDuration(c.BrandlessPhysicalPollInterval)
	if err != nil || duration <= 0 {
		return 1 * time.Minute
	}
	return duration
}

func (c *Config) GetHTTPPort() int {
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil {
		return 8080
	}
	return port
}
