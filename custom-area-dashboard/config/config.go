package config

import (
	"log"
	"os"
	"strconv"
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

	// Custom Area Configuration
	CustomAreaAdminLevel int
	CustomAreaOSMID      int64
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

		// Custom Area Configuration
		CustomAreaAdminLevel: getRequiredEnvAsInt("CUSTOM_AREA_ADMIN_LEVEL"),
		CustomAreaOSMID:      getRequiredEnvAsInt64("CUSTOM_AREA_OSM_ID"),
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getRequiredEnvAsInt(key string) int {
	if value := os.Getenv(key); value != "" {
		intValue, err := strconv.Atoi(value)
		if err == nil {
			return intValue
		}
		log.Fatalf("Cannot parse %s as int", key)
	}
	log.Fatalf("The %s value is required", key)
	return 0
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
