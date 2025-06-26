package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

	// Validate Stripe configuration
	if cfg.StripeSecretKey == "" {
		log.Printf("ERROR: STRIPE_SECRET_KEY not configured. Stripe operations will fail.")
		log.Printf("Please set STRIPE_SECRET_KEY environment variable")
	} else {
		// Log that Stripe is configured (without exposing the full key)
		log.Printf("Stripe configured with key: %s***", cfg.StripeSecretKey[:7])
	}

	if cfg.StripeWebhookSecret == "" {
		log.Printf("WARNING: STRIPE_WEBHOOK_SECRET not configured. Webhook signature verification will fail.")
	}

	// Check if any prices are missing
	missingPrices := []string{}
	for plan, priceID := range cfg.StripePrices {
		if priceID == "" {
			missingPrices = append(missingPrices, fmt.Sprintf("STRIPE_PRICE_%s", strings.ToUpper(plan)))
		}
	}
	if len(missingPrices) > 0 {
		log.Printf("WARNING: Missing Stripe price IDs: %s", strings.Join(missingPrices, ", "))
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}