package config

import (
	"cleanapp-common/appenv"
	"fmt"
	"time"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	PollInterval time.Duration

	OpenAIAPIKey string
	OpenAIModel  string

	FaceDetectorURL       string
	FaceDetectorPortStart int
	FaceDetectorCount     int

	ImagePlaceholderPath string

	BatchSize  int
	MaxWorkers int

	RabbitMQHost             string
	RabbitMQPort             string
	RabbitMQUser             string
	RabbitMQPassword         string
	RabbitMQExchange         string
	RabbitMQQueue            string
	RabbitMQReportRoutingKey string
	RabbitMQUserRoutingKey   string
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

	config := &Config{
		DBHost:     appenv.String("DB_HOST", "localhost"),
		DBPort:     appenv.String("DB_PORT", "3306"),
		DBUser:     appenv.String("DB_USER", "server"),
		DBPassword: dbPassword,
		DBName:     appenv.String("DB_NAME", "cleanapp"),

		PollInterval:             appenv.Duration("POLL_INTERVAL", 60*time.Second),
		OpenAIAPIKey:             appenv.String("OPENAI_API_KEY", ""),
		OpenAIModel:              appenv.String("OPENAI_MODEL", "gpt-5"),
		FaceDetectorURL:          appenv.String("FACE_DETECTOR_URL", "http://localhost:8000"),
		FaceDetectorPortStart:    appenv.Int("FACE_DETECTOR_PORT_START", 9500),
		FaceDetectorCount:        appenv.Int("FACE_DETECTOR_COUNT", 10),
		ImagePlaceholderPath:     appenv.String("IMAGE_PLACEHOLDER_PATH", "./image_placeholder.jpg"),
		BatchSize:                appenv.Int("BATCH_SIZE", 10),
		MaxWorkers:               appenv.Int("MAX_WORKERS", 10),
		RabbitMQHost:             appenv.String("AMQP_HOST", "localhost"),
		RabbitMQPort:             appenv.String("AMQP_PORT", "5672"),
		RabbitMQUser:             amqpUser,
		RabbitMQPassword:         amqpPassword,
		RabbitMQExchange:         appenv.String("RABBITMQ_EXCHANGE", "cleanapp-exchange"),
		RabbitMQQueue:            appenv.String("RABBITMQ_QUEUE", "gdpr-queue"),
		RabbitMQReportRoutingKey: appenv.String("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw"),
		RabbitMQUserRoutingKey:   appenv.String("RABBITMQ_USER_ROUTING_KEY", "user.add"),
	}

	return config, nil
}

func (c *Config) GetRabbitMQURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%s", c.RabbitMQUser, c.RabbitMQPassword, c.RabbitMQHost, c.RabbitMQPort)
}
