package appenv

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

func Current() Environment {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	switch value {
	case "dev", "development", "local":
		return EnvironmentDevelopment
	case "test", "testing":
		return EnvironmentTest
	case "prod", "production", "":
		if os.Getenv("CI") == "true" {
			return EnvironmentTest
		}
		return EnvironmentProduction
	default:
		return EnvironmentProduction
	}
}

func IsDevLike() bool {
	env := Current()
	return env == EnvironmentDevelopment || env == EnvironmentTest
}

func String(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func StringRequiredInProd(key, devDefault string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value, nil
	}
	if IsDevLike() && devDefault != "" {
		return devDefault, nil
	}
	return "", fmt.Errorf("%s is required", key)
}

func Strings(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func Int(key string, defaultValue int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Int64(key string, defaultValue int64) int64 {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Float64(key string, defaultValue float64) float64 {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Bool(key string, defaultValue bool) bool {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Duration(key string, defaultValue time.Duration) time.Duration {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Secret(key, devDefault string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value, nil
	}
	if IsDevLike() && devDefault != "" {
		return devDefault, nil
	}
	return "", fmt.Errorf("%s is required", key)
}

func RequiredNonDefault(key string, insecureValues ...string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	for _, insecure := range insecureValues {
		if value == insecure {
			return "", fmt.Errorf("%s must not use insecure default %q", key, insecure)
		}
	}
	return value, nil
}

func DefaultRunMigrations() bool {
	return IsDevLike()
}
