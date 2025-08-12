package config

import (
	"os"
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

	return cfg
}

// getEnv gets an environment variable with a fallback default value
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
