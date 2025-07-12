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

	// OAuth Configuration
	GoogleClientID     string
	GoogleClientSecret string
	FacebookAppID      string
	FacebookAppSecret  string
	AppleClientID      string
	AppleTeamID        string
	AppleKeyID         string
	ApplePrivateKey    string
}

func Load() *Config {
	cfg := &Config{
		DBUser:             getEnv("DB_USER", "root"),
		DBPassword:         getEnv("DB_PASSWORD", "password"),
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             getEnv("DB_PORT", "3306"),
		JWTSecret:          getEnv("JWT_SECRET", "your-secret-key-here"),
		Port:               getEnv("PORT", "8080"),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		FacebookAppID:      getEnv("FACEBOOK_APP_ID", ""),
		FacebookAppSecret:  getEnv("FACEBOOK_APP_SECRET", ""),
		AppleClientID:      getEnv("APPLE_CLIENT_ID", ""),
		AppleTeamID:        getEnv("APPLE_TEAM_ID", ""),
		AppleKeyID:         getEnv("APPLE_KEY_ID", ""),
		ApplePrivateKey:    getEnv("APPLE_PRIVATE_KEY", ""),
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
