package config

import (
	"fmt"
	"strings"

	"cleanapp-common/appenv"
)

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	Port           string
	TrustedProxies []string
	AllowedOrigins []string
	RateLimitRPS   float64
	RateLimitBurst int

	AuthServiceURL string
	JWTSecret      string

	StripeSecretKey     string
	StripeWebhookSecret string
	StripePrices        map[string]string
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
	cfg := &Config{
		DBUser:         appenv.String("DB_USER", "root"),
		DBPassword:     dbPassword,
		DBHost:         appenv.String("DB_HOST", "localhost"),
		DBPort:         appenv.String("DB_PORT", "3306"),
		Port:           appenv.String("PORT", "8080"),
		AllowedOrigins: customerAllowedOrigins(),
		RateLimitRPS:   appenv.Float64("RATE_LIMIT_RPS", 10),
		RateLimitBurst: appenv.Int("RATE_LIMIT_BURST", 20),

		AuthServiceURL:      appenv.String("AUTH_SERVICE_URL", "http://auth-service:8080"),
		JWTSecret:           jwtSecret,
		StripeSecretKey:     appenv.String("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: appenv.String("STRIPE_WEBHOOK_SECRET", ""),
		StripePrices: map[string]string{
			"base_monthly":     appenv.String("STRIPE_PRICE_BASE_MONTHLY", ""),
			"base_annual":      appenv.String("STRIPE_PRICE_BASE_ANNUAL", ""),
			"advanced_monthly": appenv.String("STRIPE_PRICE_ADVANCED_MONTHLY", ""),
			"advanced_annual":  appenv.String("STRIPE_PRICE_ADVANCED_ANNUAL", ""),
		},
	}

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
	if cfg.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET must not be empty")
	}
	return nil
}
