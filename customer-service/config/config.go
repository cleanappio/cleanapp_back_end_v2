package config

import (
	"fmt"
	"strings"

	"cleanapp-common/appenv"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	// Server
	Port            string
	TrustedProxies  []string
	AllowedOrigins  []string
	RunDBMigrations bool
	RateLimitRPS    float64
	RateLimitBurst  int

	// Auth Service
	AuthServiceURL         string
	JWTSecret              string
	AuthValidationCacheTTL string

	// Stripe
	StripeSecretKey     string
	StripeWebhookSecret string
	StripePrices        map[string]string // Map of plan_billing to price ID
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "password")
	if err != nil {
		return nil, err
	}
	jwtSecret, err := appenv.Secret("JWT_SECRET", "dev-jwt-secret")
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		DBUser:          appenv.String("DB_USER", "root"),
		DBPassword:      dbPassword,
		DBHost:          appenv.String("DB_HOST", "localhost"),
		DBPort:          appenv.String("DB_PORT", "3306"),
		Port:            appenv.String("PORT", "8080"),
		AllowedOrigins:  customerAllowedOrigins(),
		RunDBMigrations: appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
		RateLimitRPS:    float64(appenv.Int("RATE_LIMIT_RPS", 10)),
		RateLimitBurst:  appenv.Int("RATE_LIMIT_BURST", 20),

		AuthServiceURL:         appenv.String("AUTH_SERVICE_URL", "http://auth-service:8080"),
		JWTSecret:              jwtSecret,
		AuthValidationCacheTTL: appenv.String("AUTH_VALIDATION_CACHE_TTL", "30s"),
		StripeSecretKey:        appenv.String("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:    appenv.String("STRIPE_WEBHOOK_SECRET", ""),
		StripePrices: map[string]string{
			"base_monthly":     appenv.String("STRIPE_PRICE_BASE_MONTHLY", ""),
			"base_annual":      appenv.String("STRIPE_PRICE_BASE_ANNUAL", ""),
			"advanced_monthly": appenv.String("STRIPE_PRICE_ADVANCED_MONTHLY", ""),
			"advanced_annual":  appenv.String("STRIPE_PRICE_ADVANCED_ANNUAL", ""),
		},
	}

	// Handle trusted proxies
	if trustedProxies := appenv.Strings("TRUSTED_PROXIES"); len(trustedProxies) > 0 {
		cfg.TrustedProxies = trustedProxies
	}

	return cfg, validate(cfg)
}

func customerAllowedOrigins() []string {
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

func validate(cfg *Config) error {
	if cfg.AuthValidationCacheTTL != "" {
		return nil
	}
	return fmt.Errorf("AUTH_VALIDATION_CACHE_TTL must not be empty")
}
