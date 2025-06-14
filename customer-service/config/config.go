package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
)

type Config struct {
	// Database
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string

	// Security
	EncryptionKey string
	JWTSecret     string

	// Server
	Port string
}

func Load() *Config {
	cfg := &Config{
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", "password"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		JWTSecret:  getEnv("JWT_SECRET", "your-secret-key-here"),
		Port:       getEnv("PORT", "8080"),
	}

	// Handle encryption key
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		// Generate a random key for demo - in production, use a fixed key
		key := make([]byte, 32)
		rand.Read(key)
		encryptionKey = hex.EncodeToString(key)
		log.Printf("WARNING: Generated temporary encryption key. Set ENCRYPTION_KEY environment variable for production.")
	}
	cfg.EncryptionKey = encryptionKey

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
