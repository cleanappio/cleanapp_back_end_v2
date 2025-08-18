package config

import (
	"log"
	"os"
	"strconv"
	"strings"
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
	AuthServiceURL       string
	ReportAuthServiceURL string

	// Custom Area Configuration
	CustomAreaID     int64
	CustomAreaSubIDs []int64
}

func Load() *Config {
	cfg := &Config{
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", "password"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		Port:       getEnv("PORT", "8080"),
		Host:       getEnv("HOST", "0.0.0.0"),

		AuthServiceURL:       getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
		ReportAuthServiceURL: getEnv("REPORT_AUTH_SERVICE_URL", "http://report-auth-service:8080"),

		// Custom Area Configuration
		CustomAreaID:     getRequiredEnvAsInt64("CUSTOM_AREA_ID"),
		CustomAreaSubIDs: getRequiredEnvAsInt64Slice("CUSTOM_AREA_SUB_IDS"),
	}

	return cfg
}

func getRequiredEnvAsInt64Slice(key string) []int64 {
	if value := os.Getenv(key); value != "" {
		// Split the comma-separated string
		parts := strings.Split(value, ",")
		var result []int64

		for i, part := range parts {
			// Trim whitespace from each part
			trimmedPart := strings.TrimSpace(part)
			if trimmedPart == "" {
				continue // Skip empty parts
			}

			// Convert to int64
			intValue, err := strconv.ParseInt(trimmedPart, 10, 64)
			if err != nil {
				log.Fatalf("Cannot parse %s[%d]='%s' as int64: %v", key, i, trimmedPart, err)
			}
			result = append(result, intValue)
		}

		log.Printf("INFO: %s=%v", key, result)
		return result
	}
	log.Fatalf("The %s value is required", key)
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getRequiredEnvAsInt64(key string) int64 {
	if value := os.Getenv(key); value != "" {
		intValue, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return intValue
		}
		log.Fatalf("Cannot parse %s as int64", key)
	}
	log.Fatalf("The %s value is required", key)
	return 0
}
