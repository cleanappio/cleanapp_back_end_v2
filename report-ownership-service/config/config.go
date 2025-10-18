package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the report ownership service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Service configuration
	PollInterval time.Duration
	BatchSize    int

	// RabbitMQ configuration
	RabbitMQHost                     string
	RabbitMQPort                     string
	RabbitMQUser                     string
	RabbitMQPassword                 string
	RabbitMQExchange                 string
	RabbitMQQueue                    string
	RabbitMQAnalysedReportRoutingKey string

	// Logging
	LogLevel string
}

// Load loads configuration from environment variables
func Load() *Config {
	config := &Config{
		// Database defaults
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "server"),
		DBPassword: getEnv("DB_PASSWORD", "secret_app"),
		DBName:     getEnv("DB_NAME", "cleanapp"),

		// Service defaults
		PollInterval: getDurationEnv("POLL_INTERVAL", 30*time.Second),
		BatchSize:    getIntEnv("BATCH_SIZE", 100),

		// RabbitMQ defaults
		RabbitMQHost:                     getEnv("AMQP_HOST", "localhost"),
		RabbitMQPort:                     getEnv("AMQP_PORT", "5672"),
		RabbitMQUser:                     getEnv("AMQP_USER", "guest"),
		RabbitMQPassword:                 getEnv("AMQP_PASSWORD", "guest"),
		RabbitMQExchange:                 getEnv("RABBITMQ_EXCHANGE", "report_exchange"),
		RabbitMQQueue:                    getEnv("RABBITMQ_QUEUE", "ownership_queue"),
		RabbitMQAnalysedReportRoutingKey: getEnv("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.raw"),

		// Logging defaults
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}

	return config
}

// GetRabbitMQURL constructs the AMQP URL from individual components
func (c *Config) GetRabbitMQURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s", c.RabbitMQUser, c.RabbitMQPassword, c.RabbitMQHost, c.RabbitMQPort)
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getDurationEnv gets a duration environment variable or returns a default value
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
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
