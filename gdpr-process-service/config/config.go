package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the GDPR process service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Polling configuration
	PollInterval int // seconds between polling cycles

	// OpenAI configuration
	OpenAIAPIKey string
	OpenAIModel  string

	// Face detector service configuration
	FaceDetectorURL string

	// Parallel processing configuration
	BatchSize  int // number of users to process in each batch
	MaxWorkers int // maximum number of concurrent OpenAI API calls
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

		// Polling defaults
		PollInterval: getIntEnv("POLL_INTERVAL", 60), // 60 seconds default

		// OpenAI defaults
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:  getEnv("OPENAI_MODEL", "gpt-5"),

		// Face detector defaults
		FaceDetectorURL: getEnv("FACE_DETECTOR_URL", "http://localhost:8000"),

		// Parallel processing defaults
		BatchSize:  getIntEnv("BATCH_SIZE", 10),
		MaxWorkers: getIntEnv("MAX_WORKERS", 10),
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
