package config

import (
	"strings"
	"time"

	"cleanapp-common/appenv"
)

// Config holds all configuration for the report analysis backfill service.
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	ReportAnalysisURL string
	PollInterval      time.Duration
	BatchSize         int
	LogLevel          string
	SeqEndTo          int
	Port              string
	AllowedOrigins    []string
	TrustedProxies    []string
	RateLimitRPS      float64
	RateLimitBurst    int
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	reportAnalysisURL, err := appenv.StringRequiredInProd("REPORT_ANALYSIS_URL", "http://localhost:8080")
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DBHost:            appenv.String("DB_HOST", "localhost"),
		DBPort:            appenv.String("DB_PORT", "3306"),
		DBUser:            appenv.String("DB_USER", "server"),
		DBPassword:        dbPassword,
		DBName:            appenv.String("DB_NAME", "cleanapp"),
		ReportAnalysisURL: reportAnalysisURL,
		PollInterval:      appenv.Duration("POLL_INTERVAL", time.Minute),
		BatchSize:         appenv.Int("BATCH_SIZE", 20),
		LogLevel:          appenv.String("LOG_LEVEL", "info"),
		SeqEndTo:          appenv.Int("SEQ_END_TO", 0),
		Port:              appenv.String("PORT", "8080"),
		AllowedOrigins:    defaultOrigins(),
		RateLimitRPS:      appenv.Float64("RATE_LIMIT_RPS", 10),
		RateLimitBurst:    appenv.Int("RATE_LIMIT_BURST", 20),
	}
	if trusted := appenv.Strings("TRUSTED_PROXIES"); len(trusted) > 0 {
		cfg.TrustedProxies = trusted
	}
	return cfg, nil
}

func defaultOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	frontendURL := appenv.String("FRONTEND_URL", "https://cleanapp.io")
	origins := []string{frontendURL}
	if strings.Contains(frontendURL, "://cleanapp.io") {
		origins = append(origins, strings.Replace(frontendURL, "://cleanapp.io", "://www.cleanapp.io", 1))
	}
	return origins
}
