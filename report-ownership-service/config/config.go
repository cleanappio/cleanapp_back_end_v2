package config

import (
	"fmt"
	"time"

	"cleanapp-common/appenv"
)

type Config struct {
	DBHost                           string
	DBPort                           string
	DBUser                           string
	DBPassword                       string
	DBName                           string
	PollInterval                     time.Duration
	BatchSize                        int
	RabbitMQHost                     string
	RabbitMQPort                     string
	RabbitMQUser                     string
	RabbitMQPassword                 string
	RabbitMQExchange                 string
	RabbitMQQueue                    string
	RabbitMQAnalysedReportRoutingKey string
	LogLevel                         string
	RunDBMigrations                  bool
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "")
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
	return &Config{
		DBHost:                           appenv.String("DB_HOST", "localhost"),
		DBPort:                           appenv.String("DB_PORT", "3306"),
		DBUser:                           appenv.String("DB_USER", "server"),
		DBPassword:                       dbPassword,
		DBName:                           appenv.String("DB_NAME", "cleanapp"),
		PollInterval:                     appenv.Duration("POLL_INTERVAL", 30*time.Second),
		BatchSize:                        appenv.Int("BATCH_SIZE", 100),
		RabbitMQHost:                     appenv.String("AMQP_HOST", "localhost"),
		RabbitMQPort:                     appenv.String("AMQP_PORT", "5672"),
		RabbitMQUser:                     amqpUser,
		RabbitMQPassword:                 amqpPassword,
		RabbitMQExchange:                 appenv.String("RABBITMQ_EXCHANGE", "report_exchange"),
		RabbitMQQueue:                    appenv.String("RABBITMQ_QUEUE", "ownership_queue"),
		RabbitMQAnalysedReportRoutingKey: appenv.String("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.analysed"),
		LogLevel:                         appenv.String("LOG_LEVEL", "info"),
		RunDBMigrations:                  appenv.Bool("DB_RUN_MIGRATIONS", appenv.DefaultRunMigrations()),
	}, nil
}

func (c *Config) GetRabbitMQURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s", c.RabbitMQUser, c.RabbitMQPassword, c.RabbitMQHost, c.RabbitMQPort)
}
