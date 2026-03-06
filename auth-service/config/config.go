package config

import (
	"strings"

	"cleanapp-common/appenv"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	// Security
	EncryptionKey string
	JWTSecret     string

	// Server
	Port            string
	TrustedProxies  []string
	AllowedOrigins  []string
	RunDBMigrations bool
	RateLimitRPS    float64
	RateLimitBurst  int

	// OAuth Configuration
	GoogleClientID     string
	GoogleClientSecret string
	FacebookAppID      string
	FacebookAppSecret  string
	AppleClientID      string
	AppleTeamID        string
	AppleKeyID         string
	ApplePrivateKey    string

	// Email Configuration (SendGrid)
	SendGridAPIKey    string
	SendGridFromName  string
	SendGridFromEmail string

	// Frontend URL for password reset links
	FrontendURL string
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	jwtSecret, err := appenv.Secret("JWT_SECRET", "")
	if err != nil {
		return nil, err
	}
	encryptionKey, err := appenv.Secret("ENCRYPTION_KEY", devEncryptionKey())
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		DBUser:             appenv.String("DB_USER", "root"),
		DBPassword:         dbPassword,
		DBHost:             appenv.String("DB_HOST", "localhost"),
		DBPort:             appenv.String("DB_PORT", "3306"),
		JWTSecret:          jwtSecret,
		EncryptionKey:      encryptionKey,
		Port:               appenv.String("PORT", "8080"),
		GoogleClientID:     appenv.String("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: appenv.String("GOOGLE_CLIENT_SECRET", ""),
		FacebookAppID:      appenv.String("FACEBOOK_APP_ID", ""),
		FacebookAppSecret:  appenv.String("FACEBOOK_APP_SECRET", ""),
		AppleClientID:      appenv.String("APPLE_CLIENT_ID", ""),
		AppleTeamID:        appenv.String("APPLE_TEAM_ID", ""),
		AppleKeyID:         appenv.String("APPLE_KEY_ID", ""),
		ApplePrivateKey:    appenv.String("APPLE_PRIVATE_KEY", ""),
		AllowedOrigins:     authAllowedOrigins(),
		RunDBMigrations:    appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
		RateLimitRPS:       float64(appenv.Int("RATE_LIMIT_RPS", 10)),
		RateLimitBurst:     appenv.Int("RATE_LIMIT_BURST", 20),
	}

	// Handle trusted proxies
	if trustedProxies := appenv.Strings("TRUSTED_PROXIES"); len(trustedProxies) > 0 {
		cfg.TrustedProxies = trustedProxies
	}

	// Email configuration (SendGrid)
	cfg.SendGridAPIKey = appenv.String("SENDGRID_API_KEY", "")
	cfg.SendGridFromName = appenv.String("SENDGRID_FROM_NAME", "CleanApp")
	cfg.SendGridFromEmail = appenv.String("SENDGRID_FROM_EMAIL", "info@cleanapp.io")

	// Frontend URL for password reset links
	cfg.FrontendURL = appenv.String("FRONTEND_URL", "https://cleanapp.io")

	return cfg, nil
}

func authAllowedOrigins() []string {
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

func devEncryptionKey() string {
	return strings.Repeat("0", 64)
}
