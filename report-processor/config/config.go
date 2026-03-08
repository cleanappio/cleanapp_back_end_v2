package config

import (
	"cleanapp-common/appenv"
	"fmt"
)

// Config holds all configuration for the report processor service
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	Port string

	JWTSecret string

	ReportsRadiusMeters float64

	OpenAIAPIKey string
	OpenAIModel  string

	ReportsSubmissionURL      string
	ReportsSubmissionWireURL  string
	ReportsSubmissionProtocol string
	ReportsSubmissionToken    string
	TagServiceURL             string

	AMQPHost                    string
	AMQPPort                    string
	AMQPUser                    string
	AMQPPassword                string
	RabbitMQExchange            string
	RabbitMQRawReportRoutingKey string

	LogLevel string

	AllowedOrigins  []string
	RateLimitRPS    float64
	RateLimitBurst  int
	RunDBMigrations bool
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	jwtSecret, err := appenv.Secret("JWT_SECRET", "")
	if err != nil {
		return nil, err
	}
	amqpUser, err := appenv.StringRequiredInProd("AMQP_USER", "")
	if err != nil {
		return nil, err
	}
	amqpPassword, err := appenv.Secret("AMQP_PASSWORD", "")
	if err != nil {
		return nil, err
	}
	config := &Config{
		DBHost:     appenv.String("DB_HOST", "localhost"),
		DBPort:     appenv.String("DB_PORT", "3306"),
		DBUser:     appenv.String("DB_USER", "server"),
		DBPassword: dbPassword,
		DBName:     appenv.String("DB_NAME", "cleanapp"),

		Port:      appenv.String("PORT", "8080"),
		JWTSecret: jwtSecret,

		ReportsRadiusMeters: appenv.Float64("REPORTS_RADIUS_METERS", 10.0),

		OpenAIAPIKey: appenv.String("OPENAI_API_KEY", ""),
		OpenAIModel:  appenv.String("OPENAI_MODEL", "gpt-4o"),

		ReportsSubmissionURL:      appenv.String("REPORTS_SUBMISSION_URL", ""),
		ReportsSubmissionWireURL:  appenv.String("REPORTS_SUBMISSION_WIRE_URL", ""),
		ReportsSubmissionProtocol: appenv.String("REPORTS_SUBMISSION_PROTOCOL", "legacy"),
		ReportsSubmissionToken:    appenv.String("REPORTS_SUBMISSION_TOKEN", ""),
		TagServiceURL:             appenv.String("TAG_SERVICE_URL", "http://localhost:8083"),

		AMQPHost:                    appenv.String("AMQP_HOST", "localhost"),
		AMQPPort:                    appenv.String("AMQP_PORT", "5672"),
		AMQPUser:                    amqpUser,
		AMQPPassword:                amqpPassword,
		RabbitMQExchange:            appenv.String("RABBITMQ_EXCHANGE", "cleanapp"),
		RabbitMQRawReportRoutingKey: appenv.String("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw"),

		LogLevel: appenv.String("LOG_LEVEL", "info"),

		AllowedOrigins:  defaultOrigins(),
		RateLimitRPS:    appenv.Float64("RATE_LIMIT_RPS", 20),
		RateLimitBurst:  appenv.Int("RATE_LIMIT_BURST", 40),
		RunDBMigrations: appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
	}

	return config, nil
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
