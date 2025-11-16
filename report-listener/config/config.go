package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the report listener service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server configuration
	Port string

	// Broadcast configuration
	BroadcastInterval time.Duration

	// Logging
	LogLevel string

	// RabbitMQ configuration
	AMQPHost                       string
	AMQPPort                       string
	AMQPUser                       string
	AMQPPassword                   string
	RabbitExchange                 string
	RabbitAnalysedReportRoutingKey string
	RabbitTwitterReplyRoutingKey   string
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

		// Server defaults
		Port: getEnv("PORT", "8080"),

		// Broadcast defaults (1 second)
		BroadcastInterval: getDurationEnv("BROADCAST_INTERVAL", time.Second),

		// Logging defaults
		LogLevel: getEnv("LOG_LEVEL", "info"),

		// RabbitMQ defaults
		AMQPHost:                       getEnv("AMQP_HOST", "rabbitmq"),
		AMQPPort:                       getEnv("AMQP_PORT", "5672"),
		AMQPUser:                       getEnv("AMQP_USER", "guest"),
		AMQPPassword:                   getEnv("AMQP_PASSWORD", "guest"),
		RabbitExchange:                 getEnv("RABBITMQ_EXCHANGE", "cleanapp"),
		RabbitAnalysedReportRoutingKey: getEnv("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.analysed"),
		RabbitTwitterReplyRoutingKey:   getEnv("RABBITMQ_TWITTER_REPLY_ROUTING_KEY", "twitter.reply"),
	}

	return config
}

// AMQPURL builds the AMQP URL from parts
func (c *Config) AMQPURL() string {
	return "amqp://" + c.AMQPUser + ":" + c.AMQPPassword + "@" + c.AMQPHost + ":" + c.AMQPPort
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
