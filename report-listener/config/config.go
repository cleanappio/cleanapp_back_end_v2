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

	Port                              string
	RequestBodyLimitBytes             int64
	AllowedOrigins                    []string
	WebSocketAllowedOrigins           []string
	TrustedProxies                    []string
	RateLimitRPS                      float64
	RateLimitBurst                    int
	PublicDiscoveryRateLimitRPS       float64
	PublicDiscoveryRateLimitBurst     int
	PublicResolveRateLimitRPS         float64
	PublicResolveRateLimitBurst       int
	PublicDetailRateLimitRPS          float64
	PublicDetailRateLimitBurst        int
	PublicWebSocketRateLimitRPS       float64
	PublicWebSocketRateLimitBurst     int
	PrivilegedWebSocketRateLimitRPS   float64
	PrivilegedWebSocketRateLimitBurst int
	PublicDetailAbuseWindow           time.Duration
	PublicDetailAbuseMaxHits          int
	PublicDetailAbuseMaxMisses        int
	PublicDiscoveryTokenTTL           time.Duration

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
	IntelligenceRateLimitRPS    float64
	IntelligenceRateLimitBurst  int
	IntelligenceBaseURL         string
	EmailServiceURL             string
	GooglePlacesAPIKey          string
	GooglePlacesBaseURL         string

	FetcherKeyEnv                  string
	FetcherRegisterMaxPerHourPerIP int
	FetcherIngestMaxBatchItems     int
	FetcherIngestMaxBodyBytes      int64
	FetcherSelfRegistrationEnabled bool
	InternalAdminToken             string

	HumanIngestEnabled              bool
	HumanIngestRateLimitRPS         float64
	HumanIngestRateLimitBurst       int
	HumanIngestReportSourcePrefix   string
	HumanIngestReceiptLookupEnabled bool
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

		Port:                              appenv.String("PORT", "8080"),
		RequestBodyLimitBytes:             appenv.Int64("REQUEST_BODY_LIMIT_BYTES", 2*1024*1024),
		AllowedOrigins:                    defaultOrigins(),
		WebSocketAllowedOrigins:           defaultWSOrigins(),
		TrustedProxies:                    appenv.Strings("TRUSTED_PROXIES"),
		RateLimitRPS:                      appenv.Float64("RATE_LIMIT_RPS", 20),
		RateLimitBurst:                    appenv.Int("RATE_LIMIT_BURST", 40),
		PublicDiscoveryRateLimitRPS:       appenv.Float64("PUBLIC_DISCOVERY_RATE_LIMIT_RPS", 4),
		PublicDiscoveryRateLimitBurst:     appenv.Int("PUBLIC_DISCOVERY_RATE_LIMIT_BURST", 16),
		PublicResolveRateLimitRPS:         appenv.Float64("PUBLIC_RESOLVE_RATE_LIMIT_RPS", 2),
		PublicResolveRateLimitBurst:       appenv.Int("PUBLIC_RESOLVE_RATE_LIMIT_BURST", 8),
		PublicDetailRateLimitRPS:          appenv.Float64("PUBLIC_DETAIL_RATE_LIMIT_RPS", 1.5),
		PublicDetailRateLimitBurst:        appenv.Int("PUBLIC_DETAIL_RATE_LIMIT_BURST", 8),
		PublicWebSocketRateLimitRPS:       appenv.Float64("PUBLIC_WEBSOCKET_RATE_LIMIT_RPS", 1),
		PublicWebSocketRateLimitBurst:     appenv.Int("PUBLIC_WEBSOCKET_RATE_LIMIT_BURST", 4),
		PrivilegedWebSocketRateLimitRPS:   appenv.Float64("PRIVILEGED_WEBSOCKET_RATE_LIMIT_RPS", 1),
		PrivilegedWebSocketRateLimitBurst: appenv.Int("PRIVILEGED_WEBSOCKET_RATE_LIMIT_BURST", 8),
		PublicDetailAbuseWindow:           appenv.Duration("PUBLIC_DETAIL_ABUSE_WINDOW", 10*time.Minute),
		PublicDetailAbuseMaxHits:          appenv.Int("PUBLIC_DETAIL_ABUSE_MAX_HITS", 60),
		PublicDetailAbuseMaxMisses:        appenv.Int("PUBLIC_DETAIL_ABUSE_MAX_MISSES", 12),
		PublicDiscoveryTokenTTL:           appenv.Duration("PUBLIC_DISCOVERY_TOKEN_TTL", 15*time.Minute),

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
		IntelligenceRateLimitRPS:    appenv.Float64("INTELLIGENCE_RATE_LIMIT_RPS", 0.5),
		IntelligenceRateLimitBurst:  appenv.Int("INTELLIGENCE_RATE_LIMIT_BURST", 6),
		IntelligenceBaseURL:         strings.TrimRight(appenv.String("INTELLIGENCE_BASE_URL", "https://cleanapp.io"), "/"),
		EmailServiceURL:             strings.TrimRight(appenv.String("EMAIL_SERVICE_URL", "http://cleanapp_email_service:8080"), "/"),
		GooglePlacesAPIKey:          appenv.String("GOOGLE_PLACES_API_KEY", ""),
		GooglePlacesBaseURL:         strings.TrimRight(appenv.String("GOOGLE_PLACES_BASE_URL", "https://places.googleapis.com/v1"), "/"),

		FetcherKeyEnv:                  strings.ToLower(appenv.String("FETCHER_KEY_ENV", "live")),
		FetcherRegisterMaxPerHourPerIP: appenv.Int("FETCHER_REGISTER_MAX_PER_HOUR_PER_IP", 5),
		FetcherIngestMaxBatchItems:     appenv.Int("FETCHER_INGEST_MAX_BATCH_ITEMS", 100),
		FetcherIngestMaxBodyBytes:      appenv.Int64("FETCHER_INGEST_MAX_BODY_BYTES", 2*1024*1024),
		FetcherSelfRegistrationEnabled: appenv.Bool("FETCHER_SELF_REGISTRATION_ENABLED", appenv.IsDevLike()),
		InternalAdminToken:             appenv.String("INTERNAL_ADMIN_TOKEN", ""),

		HumanIngestEnabled:              appenv.Bool("HUMAN_INGEST_ENABLED", true),
		HumanIngestRateLimitRPS:         appenv.Float64("HUMAN_INGEST_RATE_LIMIT_RPS", 0.5),
		HumanIngestRateLimitBurst:       appenv.Int("HUMAN_INGEST_RATE_LIMIT_BURST", 5),
		HumanIngestReportSourcePrefix:   appenv.String("HUMAN_INGEST_SOURCE_PREFIX", "human"),
		HumanIngestReceiptLookupEnabled: appenv.Bool("HUMAN_INGEST_RECEIPT_LOOKUP_ENABLED", true),

		CleanAppWireEnabled:             appenv.Bool("CLEANAPP_WIRE_ENABLED", true),
		CleanAppWireBatchEnabled:        appenv.Bool("CLEANAPP_WIRE_BATCH_ENABLED", true),
		CleanAppWirePriorityLaneEnabled: appenv.Bool("CLEANAPP_WIRE_PRIORITY_LANE_ENABLED", false),
		CleanAppWireStrictSignature:     appenv.Bool("CLEANAPP_WIRE_STRICT_SIGNATURE", false),
		CleanAppWirePublishLaneMinTier:  appenv.Int("CLEANAPP_WIRE_PUBLISH_LANE_MIN_TIER", 2),
	}

	if config.BroadcastInterval <= 0 {
		config.BroadcastInterval = time.Second
	}
	if config.PublicDetailAbuseWindow <= 0 {
		config.PublicDetailAbuseWindow = 10 * time.Minute
	}
	if config.PublicDiscoveryTokenTTL <= 0 {
		config.PublicDiscoveryTokenTTL = 15 * time.Minute
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
