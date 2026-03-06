package config

import (
	"log"
	"strconv"
	"strings"

	"cleanapp-common/appenv"
)

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	Port       string
	Host       string

	AuthServiceURL       string
	ReportAuthServiceURL string

	CustomAreaID     int64
	CustomAreaSubIDs []int64
	IsPublic         bool

	TrustedProxies         []string
	AllowedOrigins         []string
	WebSocketAllowedOrigins []string
	RateLimitRPS           float64
	RateLimitBurst         int
}

func Load() (*Config, error) {
	dbPassword, err := appenv.Secret("DB_PASSWORD", "password")
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		DBUser:               appenv.String("DB_USER", "root"),
		DBPassword:           dbPassword,
		DBHost:               appenv.String("DB_HOST", "localhost"),
		DBPort:               appenv.String("DB_PORT", "3306"),
		Port:                 appenv.String("PORT", "8080"),
		Host:                 appenv.String("HOST", "0.0.0.0"),
		AuthServiceURL:       appenv.String("AUTH_SERVICE_URL", "http://auth-service:8080"),
		ReportAuthServiceURL: appenv.String("REPORT_AUTH_SERVICE_URL", "http://report-auth-service:8080"),
		CustomAreaID:         getRequiredEnvAsInt64("CUSTOM_AREA_ID"),
		CustomAreaSubIDs:     getRequiredEnvAsInt64Slice("CUSTOM_AREA_SUB_IDS"),
		IsPublic:             appenv.Bool("IS_PUBLIC", false),
		AllowedOrigins:       allowedOrigins(),
		RateLimitRPS:         float64(appenv.Int("RATE_LIMIT_RPS", 10)),
		RateLimitBurst:       appenv.Int("RATE_LIMIT_BURST", 20),
	}
	cfg.WebSocketAllowedOrigins = cfg.AllowedOrigins
	if trusted := appenv.Strings("TRUSTED_PROXIES"); len(trusted) > 0 {
		cfg.TrustedProxies = trusted
	}
	if wsOrigins := appenv.Strings("WEBSOCKET_ALLOWED_ORIGINS"); len(wsOrigins) > 0 {
		cfg.WebSocketAllowedOrigins = wsOrigins
	}
	return cfg, nil
}

func allowedOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	frontendURL := appenv.String("FRONTEND_URL", "https://cleanapp.io")
	origins := []string{frontendURL}
	if strings.Contains(frontendURL, "://cleanapp.io") {
		origins = append(origins, strings.Replace(frontendURL, "://cleanapp.io", "://www.cleanapp.io", 1))
	}
	return origins
}

func getRequiredEnvAsInt64Slice(key string) []int64 {
	value := strings.TrimSpace(appenv.String(key, ""))
	if value == "" {
		log.Fatalf("The %s value is required", key)
	}
	parts := strings.Split(value, ",")
	var result []int64
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		intValue, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			log.Fatalf("Cannot parse %s[%d]='%s' as int64: %v", key, i, trimmed, err)
		}
		result = append(result, intValue)
	}
	log.Printf("INFO: %s=%v", key, result)
	return result
}

func getRequiredEnvAsInt64(key string) int64 {
	value := strings.TrimSpace(appenv.String(key, ""))
	if value == "" {
		log.Fatalf("The %s value is required", key)
	}
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		log.Fatalf("Cannot parse %s as int64", key)
	}
	return intValue
}
