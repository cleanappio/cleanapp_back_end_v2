package config

import (
	"strconv"
	"strings"

	"cleanapp-common/appenv"
)

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBName     string
	Port       string
	Host       string

	JWTSecret string

	CustomAreaID     int64
	CustomAreaSubIDs []int64
	IsPublic         bool

	TrustedProxies          []string
	AllowedOrigins          []string
	WebSocketAllowedOrigins []string
	RateLimitRPS            float64
	RateLimitBurst          int
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
	cfg := &Config{
		DBUser:           appenv.String("DB_USER", "server"),
		DBPassword:       dbPassword,
		DBHost:           appenv.String("DB_HOST", "localhost"),
		DBPort:           appenv.String("DB_PORT", "3306"),
		DBName:           appenv.String("DB_NAME", "cleanapp"),
		Port:             appenv.String("PORT", "8080"),
		Host:             appenv.String("HOST", "0.0.0.0"),
		JWTSecret:        jwtSecret,
		CustomAreaID:     getRequiredEnvAsInt64("CUSTOM_AREA_ID"),
		CustomAreaSubIDs: getRequiredEnvAsInt64Slice("CUSTOM_AREA_SUB_IDS"),
		IsPublic:         appenv.Bool("IS_PUBLIC", false),
		AllowedOrigins:   allowedOrigins(),
		RateLimitRPS:     appenv.Float64("RATE_LIMIT_RPS", 10),
		RateLimitBurst:   appenv.Int("RATE_LIMIT_BURST", 20),
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
		panic(key + " is required")
	}
	parts := strings.Split(value, ",")
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		intValue, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			panic("cannot parse " + key + " as int64 slice")
		}
		result = append(result, intValue)
	}
	return result
}

func getRequiredEnvAsInt64(key string) int64 {
	value := strings.TrimSpace(appenv.String(key, ""))
	if value == "" {
		panic(key + " is required")
	}
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		panic("cannot parse " + key + " as int64")
	}
	return intValue
}
