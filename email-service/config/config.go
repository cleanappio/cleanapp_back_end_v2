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
	cfg.DBHost = getEnv("MYSQL_HOST", "localhost")
	cfg.DBPort = getEnv("MYSQL_PORT", "3306")
	cfg.DBUser = getEnv("MYSQL_USER", "server")
	cfg.DBPassword = getEnv("MYSQL_PASSWORD", "secret")
	cfg.DBName = getEnv("MYSQL_DB", "cleanapp")

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
