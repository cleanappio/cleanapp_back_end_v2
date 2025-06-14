package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strings"
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
	Port           string
	TrustedProxies []string

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
		JWTSecret:           getEnv("JWT_SECRET", "your-secret-key-here"),
		Port:                getEnv("PORT", "8080"),
		StripeSecretKey:     getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripePrices: map[string]string{
			"base_monthly":      getEnv("STRIPE_PRICE_BASE_MONTHLY", ""),
			"base_annual":       getEnv("STRIPE_PRICE_BASE_ANNUAL", ""),
			"advanced_monthly":  getEnv("STRIPE_PRICE_ADVANCED_MONTHLY", ""),
			"advanced_annual":   getEnv("STRIPE_PRICE_ADVANCED_ANNUAL", ""),
			"exclusive_monthly": getEnv("STRIPE_PRICE_EXCLUSIVE_MONTHLY", ""),
			"exclusive_annual":  getEnv("STRIPE_PRICE_EXCLUSIVE_ANNUAL", ""),
		},
	}

	// Handle encryption key
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		// Generate a random key for demo - in production, use a fixed key
		key := make([]byte, 32)
		rand.Read(key)
		encryptionKey = hex.EncodeToString(key)
		log.Printf("WARNING: Generated temporary encryption key. Set ENCRYPTION_KEY environment variable for production.")
	}
	cfg.EncryptionKey = encryptionKey

	// Handle trusted proxies
	trustedProxies := os.Getenv("TRUSTED_PROXIES")
	if trustedProxies == "" {
		// Default to localhost only
		cfg.TrustedProxies = []string{"127.0.0.1", "::1"}
	} else {
		// Split comma-separated values and trim spaces
		proxies := strings.Split(trustedProxies, ",")
		cfg.TrustedProxies = make([]string, 0, len(proxies))
		for _, proxy := range proxies {
			if trimmed := strings.TrimSpace(proxy); trimmed != "" {
				cfg.TrustedProxies = append(cfg.TrustedProxies, trimmed)
			}
		}
	}

	// Warn if Stripe is not configured
	if cfg.StripeSecretKey == "" {
		log.Printf("WARNING: Stripe secret key not configured. Payment processing will not work.")
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
