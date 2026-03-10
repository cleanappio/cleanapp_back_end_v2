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
	JWTSecret  string

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
	EmailServiceURL             string

	FetcherKeyEnv                  string
	FetcherRegisterMaxPerHourPerIP int
	FetcherIngestMaxBatchItems     int
	FetcherIngestMaxBodyBytes      int64
	InternalAdminToken             string

	CleanAppWireEnabled             bool
	CleanAppWireBatchEnabled        bool
	CleanAppWirePriorityLaneEnabled bool
	CleanAppWireStrictSignature     bool
	CleanAppWirePublishLaneMinTier  int
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
		JWTSecret:  jwtSecret,

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
		EmailServiceURL:             strings.TrimRight(appenv.String("EMAIL_SERVICE_URL", "http://cleanapp_email_service:8080"), "/"),

		FetcherKeyEnv:                  strings.ToLower(appenv.String("FETCHER_KEY_ENV", "live")),
		FetcherRegisterMaxPerHourPerIP: appenv.Int("FETCHER_REGISTER_MAX_PER_HOUR_PER_IP", 5),
		FetcherIngestMaxBatchItems:     appenv.Int("FETCHER_INGEST_MAX_BATCH_ITEMS", 100),
		FetcherIngestMaxBodyBytes:      appenv.Int64("FETCHER_INGEST_MAX_BODY_BYTES", 2*1024*1024),
		InternalAdminToken:             appenv.String("INTERNAL_ADMIN_TOKEN", ""),

		CleanAppWireEnabled:             appenv.Bool("CLEANAPP_WIRE_ENABLED", true),
		CleanAppWireBatchEnabled:        appenv.Bool("CLEANAPP_WIRE_BATCH_ENABLED", true),
		CleanAppWirePriorityLaneEnabled: appenv.Bool("CLEANAPP_WIRE_PRIORITY_LANE_ENABLED", false),
		CleanAppWireStrictSignature:     appenv.Bool("CLEANAPP_WIRE_STRICT_SIGNATURE", false),
		CleanAppWirePublishLaneMinTier:  appenv.Int("CLEANAPP_WIRE_PUBLISH_LANE_MIN_TIER", 2),
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
