package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the areas service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server configuration
	Port string

	// Auth Service configuration
	AuthServiceURL string
}

// Load loads configuration from environment variables
func Load() *Config {
	config := &Config{
		// Database defaults
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "server"),
		DBPassword: getEnv("DB_PASSWORD", "secret"),
		DBName:     getEnv("DB_NAME", "cleanapp"),

		// Server defaults
		Port: getEnv("PORT", "8080"),

		// Auth Service defaults
		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
	}

	return config
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getIntEnv gets an integer environment variable or returns a default value
func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
