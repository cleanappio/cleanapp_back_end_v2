package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the report analyze pipeline service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server configuration
	Port string

	// OpenAI configuration
	OpenAIAPIKey string
	OpenAIModel  string

	// Analysis configuration
	AnalysisInterval time.Duration
	MaxRetries       int
	AnalysisPrompt   string

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

		// OpenAI defaults
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:  getEnv("OPENAI_MODEL", "gpt-4o"),

		// Analysis defaults (30 seconds)
		AnalysisInterval: getDurationEnv("ANALYSIS_INTERVAL", 30*time.Second),
		MaxRetries:       getIntEnv("MAX_RETRIES", 3),
		AnalysisPrompt:   getEnv("ANALYSIS_PROMPT", "What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo and a severity level from 0.0 to 1.0."),

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
