package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the report analysis backfill service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Analysis API configuration
	ReportAnalysisURL string

	// Polling configuration
	PollInterval time.Duration
	BatchSize    int

	// Logging
	LogLevel string

	// End Seq
	SeqEndTo   int
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

		// Analysis API defaults
		ReportAnalysisURL: getEnv("REPORT_ANALYSIS_URL", "http://localhost:8080"),

		// Polling defaults (1 minute interval, 20 reports per batch)
		PollInterval: getDurationEnv("POLL_INTERVAL", 1*time.Minute),
		BatchSize:    getIntEnv("BATCH_SIZE", 20),

		// Logging defaults
		LogLevel: getEnv("LOG_LEVEL", "info"),

		// Start and End Point
		SeqEndTo:   getIntEnv("SEQ_END_TO", 0),
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
