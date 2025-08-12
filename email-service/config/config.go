package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the email service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// SendGrid configuration
	SendGridAPIKey    string
	SendGridFromName  string
	SendGridFromEmail string

	// Service configuration
	OptOutURL    string
	PollInterval string
	HTTPPort     string
}

// Load loads configuration from environment variables and flags
func Load() *Config {
	cfg := &Config{}

	// Database configuration
	cfg.DBHost = getEnv("DB_HOST", "localhost")
	cfg.DBPort = getEnv("DB_PORT", "3306")
	cfg.DBUser = getEnv("DB_USER", "server")
	cfg.DBPassword = getEnv("DB_PASSWORD", "secret")
	cfg.DBName = getEnv("DB_NAME", "cleanapp")

	// SendGrid configuration
	cfg.SendGridAPIKey = getEnv("SENDGRID_API_KEY", "")
	cfg.SendGridFromName = getEnv("SENDGRID_FROM_NAME", "CleanApp")
	cfg.SendGridFromEmail = getEnv("SENDGRID_FROM_EMAIL", "info@cleanapp.io")

	// Service configuration
	cfg.OptOutURL = getEnv("OPT_OUT_URL", "http://localhost:8080/opt-out")
	cfg.PollInterval = getEnv("POLL_INTERVAL", "10s")
	cfg.HTTPPort = getEnv("HTTP_PORT", "8080")

	return cfg
}

// GetPollInterval returns the parsed poll interval duration
func (c *Config) GetPollInterval() time.Duration {
	duration, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		// Fallback to default 30 seconds if parsing fails
		return 30 * time.Second
	}
	return duration
}

// GetHTTPPort returns the HTTP port as an integer
func (c *Config) GetHTTPPort() int {
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil {
		// Fallback to default port 8080 if parsing fails
		return 8080
	}
	return port
}

// getEnv gets an environment variable with a fallback default value
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
