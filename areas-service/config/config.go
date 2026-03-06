package config

import (
	"strings"

	"cleanapp-common/appenv"
)

// Config holds all configuration for the areas service
type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	Port string

	JWTSecret string

	TrustedProxies []string
	AllowedOrigins []string
	RateLimitRPS   float64
	RateLimitBurst int
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
		DBHost:         appenv.String("DB_HOST", "localhost"),
		DBPort:         appenv.String("DB_PORT", "3306"),
		DBUser:         appenv.String("DB_USER", "server"),
		DBPassword:     dbPassword,
		DBName:         appenv.String("DB_NAME", "cleanapp"),
		Port:           appenv.String("PORT", "8080"),
		JWTSecret:      jwtSecret,
		AllowedOrigins: allowedOrigins(),
		RateLimitRPS:   appenv.Float64("RATE_LIMIT_RPS", 10),
		RateLimitBurst: appenv.Int("RATE_LIMIT_BURST", 20),
	}
	if trusted := appenv.Strings("TRUSTED_PROXIES"); len(trusted) > 0 {
		cfg.TrustedProxies = trusted
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
