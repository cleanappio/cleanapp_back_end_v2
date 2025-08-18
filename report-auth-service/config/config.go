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
}

func Load() *Config {
	cfg := &Config{
		DBUser:         getEnv("DB_USER", "root"),
		DBPassword:     getEnv("DB_PASSWORD", "password"),
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         getEnv("DB_PORT", "3306"),
		Port:           getEnv("PORT", "8081"),
		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://localhost:8080"),
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
