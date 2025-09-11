package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the report processor service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server configuration
	Port string

	// Auth service configuration
	AuthServiceURL string

	// Report matching configuration
	ReportsRadiusMeters float64

	// OpenAI configuration
	OpenAIAPIKey string
	OpenAIModel  string

	// Reports submission configuration
	ReportsSubmissionURL string

	// Logging
	LogLevel string
}

// Load loads configuration from environment variables
func Load() *Config {
	config := &Config{
		// Database defaults
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "server"),
		DBPassword: getEnv("DB_PASSWORD", "secret_app"),
		DBName:     getEnv("DB_NAME", "cleanapp"),

		// Server defaults
		Port: getEnv("PORT", "8080"),

		// Auth service defaults
		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://localhost:8080"),

		// Report matching defaults
		ReportsRadiusMeters: getFloatEnv("REPORTS_RADIUS_METERS", 100.0),

		// OpenAI defaults
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:  getEnv("OPENAI_MODEL", "gpt-4o"),

		// Reports submission defaults
		ReportsSubmissionURL: getEnv("REPORTS_SUBMISSION_URL", ""),

		// Logging defaults
		LogLevel: getEnv("LOG_LEVEL", "info"),
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

// getDurationEnv gets a duration environment variable or returns a default value
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
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

// getFloatEnv gets a float environment variable or returns a default value
func getFloatEnv(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
