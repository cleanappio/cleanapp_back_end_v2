package config

import (
	"os"
	"strings"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBName     string

	// Server
	Port string
	Host string

	// Auth Service
	AuthServiceURL string

	// Brand Dashboard Configuration
	BrandNames []string
}

func Load() *Config {
	cfg := &Config{
		DBUser:         getEnv("DB_USER", "server"),
		DBPassword:     getEnv("DB_PASSWORD", "secret_app"),
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         getEnv("DB_PORT", "3306"),
		DBName:         getEnv("DB_NAME", "cleanapp"),
		Port:           getEnv("PORT", "8080"),
		Host:           getEnv("HOST", "0.0.0.0"),
		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
	}

	// Load brand names from environment variable
	brandNamesStr := getEnv("BRAND_NAMES", "coca-cola,redbull,nike,adidas")
	cfg.BrandNames = parseBrandNames(brandNamesStr)

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseBrandNames(brandNamesStr string) []string {
	if brandNamesStr == "" {
		return []string{}
	}

	// Split by comma and clean up each brand name
	brands := strings.Split(brandNamesStr, ",")
	var cleanBrands []string

	for _, brand := range brands {
		cleanBrand := strings.TrimSpace(brand)
		if cleanBrand != "" {
			cleanBrands = append(cleanBrands, cleanBrand)
		}
	}

	return cleanBrands
}
