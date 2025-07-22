package config

import (
	"os"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	// Server
	Port string
	Host string

	// Auth Service
	AuthServiceURL string
}

func Load() *Config {
	cfg := &Config{
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", "password"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		Port:       getEnv("PORT", "8080"),
		Host:       getEnv("HOST", "0.0.0.0"),

		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
