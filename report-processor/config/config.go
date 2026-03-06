package config

import (
	"cleanapp-common/appenv"
	"fmt"
	"strconv"
	"time"
)

// Config holds all configuration for the report processor service
type Config struct {
	// Database configuration
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Server configuration
	Port string

	// Auth service configuration
	AuthServiceURL string

	// Report matching configuration
	ReportsRadiusMeters float64

	// OpenAI configuration
	OpenAIAPIKey string
	OpenAIModel  string

	// Reports submission configuration
	ReportsSubmissionURL string

	// Tag service configuration
	TagServiceURL string

	// RabbitMQ configuration
	AMQPHost                    string
	AMQPPort                    string
	AMQPUser                    string
	AMQPPassword                string
	RabbitMQExchange            string
	RabbitMQRawReportRoutingKey string

	// Logging
	LogLevel string

	AllowedOrigins  []string
	RateLimitRPS    float64
	RateLimitBurst  int
	RunDBMigrations bool
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "secret_app")
	if err != nil {
		return nil, err
	}
	amqpPassword, err := appenv.Secret("AMQP_PASSWORD", "guest")
	if err != nil {
		return nil, err
	}

	config := &Config{
		// Database defaults
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "server"),
		DBPassword: dbPassword,
		DBName:     getEnv("DB_NAME", "cleanapp"),

		// Server defaults
		Port: getEnv("PORT", "8080"),

		// Auth service defaults
		AuthServiceURL: getEnv("AUTH_SERVICE_URL", "http://localhost:8080"),

		// Report matching defaults
		ReportsRadiusMeters: getFloatEnv("REPORTS_RADIUS_METERS", 10.0),

		// OpenAI defaults
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:  getEnv("OPENAI_MODEL", "gpt-4o"),

		// Reports submission defaults
		ReportsSubmissionURL: getEnv("REPORTS_SUBMISSION_URL", ""),

		// Tag service defaults
		TagServiceURL: getEnv("TAG_SERVICE_URL", "http://localhost:8083"),

		// RabbitMQ defaults
		AMQPHost:                    getEnv("AMQP_HOST", "localhost"),
		AMQPPort:                    getEnv("AMQP_PORT", "5672"),
		AMQPUser:                    getEnv("AMQP_USER", "guest"),
		AMQPPassword:                amqpPassword,
		RabbitMQExchange:            getEnv("RABBITMQ_EXCHANGE", "cleanapp"),
		RabbitMQRawReportRoutingKey: getEnv("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw"),

		// Logging defaults
		LogLevel: getEnv("LOG_LEVEL", "info"),

		AllowedOrigins:  defaultOrigins(),
		RateLimitRPS:    getFloatEnv("RATE_LIMIT_RPS", 20),
		RateLimitBurst:  getIntEnv("RATE_LIMIT_BURST", 40),
		RunDBMigrations: appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
	}

	return config, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := appenv.String(key, ""); value != "" {
		return value
	}
	return defaultValue
}

// getDurationEnv gets a duration environment variable or returns a default value
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := appenv.String(key, ""); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// getIntEnv gets an integer environment variable or returns a default value
func getIntEnv(key string, defaultValue int) int {
	if value := appenv.String(key, ""); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getFloatEnv gets a float environment variable or returns a default value
func getFloatEnv(key string, defaultValue float64) float64 {
	if value := appenv.String(key, ""); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}

func defaultOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	return []string{
		"https://cleanapp.io",
		"https://www.cleanapp.io",
		"https://live.cleanapp.io",
		"http://localhost:3000",
		"http://localhost:3001",
	}
}

// GetAMQPURL constructs the AMQP connection URL from configuration
func (c *Config) GetAMQPURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s/", c.AMQPUser, c.AMQPPassword, c.AMQPHost, c.AMQPPort)
}
