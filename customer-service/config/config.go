package config

import (
	"os"
	"strings"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	// Server
	Port           string
	TrustedProxies []string

	// Auth Service
	AuthServiceURL string

	// Stripe
	StripeSecretKey     string
	StripeWebhookSecret string
	StripePrices        map[string]string // Map of plan_billing to price ID
}

func Load() *Config {
	cfg := &Config{
		DBUser:              getEnv("DB_USER", "root"),
		DBPassword:          getEnv("DB_PASSWORD", "password"),
		DBHost:              getEnv("DB_HOST", "localhost"),
		DBPort:              getEnv("DB_PORT", "3306"),
		Port:                getEnv("PORT", "8080"),
		AuthServiceURL:      getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
		StripeSecretKey:     getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripePrices: map[string]string{
			"base_monthly":     getEnv("STRIPE_PRICE_BASE_MONTHLY", ""),
			"base_annual":      getEnv("STRIPE_PRICE_BASE_ANNUAL", ""),
			"advanced_monthly": getEnv("STRIPE_PRICE_ADVANCED_MONTHLY", ""),
			"advanced_annual":  getEnv("STRIPE_PRICE_ADVANCED_ANNUAL", ""),
		},
	}

	// Handle trusted proxies
	trustedProxies := os.Getenv("TRUSTED_PROXIES")
	if trustedProxies != "" {
		cfg.TrustedProxies = strings.Split(trustedProxies, ",")
		for i, proxy := range cfg.TrustedProxies {
			cfg.TrustedProxies[i] = strings.TrimSpace(proxy)
		}
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
