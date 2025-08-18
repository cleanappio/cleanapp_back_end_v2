package config

import (
	"brand-dashboard/utils"
	"log"
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
	AuthServiceURL       string
	ReportAuthServiceURL string

	// Brand Dashboard Configuration
	BrandNames           []string
	NormailzedBrandNames []string
}

func Load() *Config {
	cfg := &Config{
		DBUser:     getEnv("DB_USER", "server"),
		DBPassword: getEnv("DB_PASSWORD", "secret_app"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBName:     getEnv("DB_NAME", "cleanapp"),
		Port:       getEnv("PORT", "8080"),
		Host:       getEnv("HOST", "0.0.0.0"),

		AuthServiceURL:       getEnv("AUTH_SERVICE_URL", "http://auth-service:8080"),
		ReportAuthServiceURL: getEnv("REPORT_AUTH_SERVICE_URL", "http://report-auth-service:8080"),
	}

	// Load brand names from environment variable
	brandNamesStr := getEnv("BRAND_NAMES", "coca-cola,redbull,nike,adidas")
	cfg.BrandNames = parseBrandNames(brandNamesStr)

	for _, brandName := range cfg.BrandNames {
		cfg.NormailzedBrandNames = append(cfg.NormailzedBrandNames, utils.NormalizeBrandName(brandName))
	}

	log.Printf("Brand names: %v", cfg.BrandNames)
	log.Printf("Normailzed brand names: %v", cfg.NormailzedBrandNames)

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

// IsBrandMatch checks if a normalized brand name matches any configured brand
func (cfg *Config) IsBrandMatch(normalizedBrandName string) (bool, string) {
	for _, brand := range cfg.NormailzedBrandNames {
		if brand == normalizedBrandName {
			return true, brand
		}
	}
	return false, ""
}
