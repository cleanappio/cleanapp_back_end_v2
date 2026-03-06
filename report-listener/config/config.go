package config

import (
	"cleanapp-common/appenv"
	"fmt"
	"strings"
	"time"
)

// Config holds all configuration for the report listener service
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	Port                    string
	RequestBodyLimitBytes   int64
	AllowedOrigins          []string
	WebSocketAllowedOrigins []string
	RateLimitRPS            float64
	RateLimitBurst          int

	BroadcastInterval time.Duration

	LogLevel string

	AMQPHost                       string
	AMQPPort                       string
	AMQPUser                       string
	AMQPPassword                   string
	RabbitExchange                 string
	RabbitRawReportRoutingKey      string
	RabbitAnalysedReportRoutingKey string
	RabbitTwitterReplyRoutingKey   string

	GeminiAPIKey                string
	GeminiModel                 string
	IntelligenceFreeTierMaxTurn int
	IntelligenceBaseURL         string

	FetcherKeyEnv                  string
	FetcherRegisterMaxPerHourPerIP int
	FetcherIngestMaxBatchItems     int
	FetcherIngestMaxBodyBytes      int64
	InternalAdminToken             string
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

		Port:                    appenv.String("PORT", "8080"),
		RequestBodyLimitBytes:   appenv.Int64("REQUEST_BODY_LIMIT_BYTES", 2*1024*1024),
		AllowedOrigins:          defaultOrigins(),
		WebSocketAllowedOrigins: defaultWSOrigins(),
		RateLimitRPS:            appenv.Float64("RATE_LIMIT_RPS", 20),
		RateLimitBurst:          appenv.Int("RATE_LIMIT_BURST", 40),

		BroadcastInterval: appenv.Duration("BROADCAST_INTERVAL", time.Second),
		LogLevel:          appenv.String("LOG_LEVEL", "info"),

		AMQPHost:                       appenv.String("AMQP_HOST", "rabbitmq"),
		AMQPPort:                       appenv.String("AMQP_PORT", "5672"),
		AMQPUser:                       amqpUser,
		AMQPPassword:                   amqpPassword,
		RabbitExchange:                 appenv.String("RABBITMQ_EXCHANGE", "cleanapp"),
		RabbitRawReportRoutingKey:      appenv.String("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw"),
		RabbitAnalysedReportRoutingKey: appenv.String("RABBITMQ_ANALYSED_REPORT_ROUTING_KEY", "report.analysed"),
		RabbitTwitterReplyRoutingKey:   appenv.String("RABBITMQ_TWITTER_REPLY_ROUTING_KEY", "twitter.reply"),

		GeminiAPIKey:                appenv.String("GEMINI_API_KEY", ""),
		GeminiModel:                 appenv.String("GEMINI_MODEL", "gemini-2.5-flash"),
		IntelligenceFreeTierMaxTurn: appenv.Int("INTELLIGENCE_FREE_MAX_TURNS", 5),
		IntelligenceBaseURL:         strings.TrimRight(appenv.String("INTELLIGENCE_BASE_URL", "https://cleanapp.io"), "/"),

		FetcherKeyEnv:                  strings.ToLower(appenv.String("FETCHER_KEY_ENV", "live")),
		FetcherRegisterMaxPerHourPerIP: appenv.Int("FETCHER_REGISTER_MAX_PER_HOUR_PER_IP", 5),
		FetcherIngestMaxBatchItems:     appenv.Int("FETCHER_INGEST_MAX_BATCH_ITEMS", 100),
		FetcherIngestMaxBodyBytes:      appenv.Int64("FETCHER_INGEST_MAX_BODY_BYTES", 2*1024*1024),
		InternalAdminToken:             appenv.String("INTERNAL_ADMIN_TOKEN", ""),
	}

	if config.BroadcastInterval <= 0 {
		config.BroadcastInterval = time.Second
	}

	return config, validate(config)
}

func (c *Config) AMQPURL() string {
	return "amqp://" + c.AMQPUser + ":" + c.AMQPPassword + "@" + c.AMQPHost + ":" + c.AMQPPort
}

func defaultOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	base := strings.TrimRight(appenv.String("INTELLIGENCE_BASE_URL", "https://cleanapp.io"), "/")
	origins := []string{base}
	if strings.Contains(base, "://cleanapp.io") {
		origins = append(origins, strings.Replace(base, "://cleanapp.io", "://www.cleanapp.io", 1))
	}
	return origins
}

func defaultWSOrigins() []string {
	if origins := appenv.Strings("WEBSOCKET_ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	return defaultOrigins()
}

func validate(cfg *Config) error {
	if cfg.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	return nil
}
