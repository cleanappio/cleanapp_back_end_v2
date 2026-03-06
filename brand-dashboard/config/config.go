package config

import (
	"strings"

	"brand-dashboard/utils"
	"cleanapp-common/appenv"
)

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBName     string

	Port string
	Host string

	JWTSecret string

	BrandNames           []string
	NormailzedBrandNames []string
	TrustedProxies       []string
	AllowedOrigins       []string
	RateLimitRPS         float64
	RateLimitBurst       int
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
		DBUser:         appenv.String("DB_USER", "server"),
		DBPassword:     dbPassword,
		DBHost:         appenv.String("DB_HOST", "localhost"),
		DBPort:         appenv.String("DB_PORT", "3306"),
		DBName:         appenv.String("DB_NAME", "cleanapp"),
		Port:           appenv.String("PORT", "8080"),
		Host:           appenv.String("HOST", "0.0.0.0"),
		JWTSecret:      jwtSecret,
		AllowedOrigins: defaultOrigins(),
		RateLimitRPS:   appenv.Float64("RATE_LIMIT_RPS", 10),
		RateLimitBurst: appenv.Int("RATE_LIMIT_BURST", 20),
	}
	if trusted := appenv.Strings("TRUSTED_PROXIES"); len(trusted) > 0 {
		cfg.TrustedProxies = trusted
	}
	brandNamesStr := appenv.String("BRAND_NAMES", "coca-cola,redbull,nike,adidas")
	cfg.BrandNames = parseBrandNames(brandNamesStr)
	for _, brandName := range cfg.BrandNames {
		cfg.NormailzedBrandNames = append(cfg.NormailzedBrandNames, utils.NormalizeBrandName(brandName))
	}
	return cfg, nil
}

func defaultOrigins() []string {
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

func parseBrandNames(brandNamesStr string) []string {
	if brandNamesStr == "" {
		return []string{}
	}
	brands := strings.Split(brandNamesStr, ",")
	cleanBrands := make([]string, 0, len(brands))
	for _, brand := range brands {
		cleanBrand := strings.TrimSpace(brand)
		if cleanBrand != "" {
			cleanBrands = append(cleanBrands, cleanBrand)
		}
	}
	return cleanBrands
}

func (cfg *Config) IsBrandMatch(normalizedBrandName string) (bool, string) {
	for _, brand := range cfg.NormailzedBrandNames {
		if brand == normalizedBrandName {
			return true, brand
		}
	}
	return false, ""
}
