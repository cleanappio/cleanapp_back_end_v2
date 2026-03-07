package config

import (
	"cleanapp-common/appenv"
	"encoding/json"
	"strings"
)

type Config struct {
	// Server configuration
	Port string

	// OpenAI configuration
	OpenAIAPIKey string
	OpenAIModel  string

	// CORS configuration
	AllowedOrigins []string

	// Rate limiting
	RateLimitRPS   float64
	RateLimitBurst int

	// TURN servers (optional)
	TurnServersJSON string
}

type TurnServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func Load() (*Config, error) {
	apiKey, err := appenv.Secret("TRASHFORMER_OPENAI_API_KEY", "")
	if err != nil {
		return nil, err
	}
	rateLimitPerMinute := appenv.Int("RATE_LIMIT_PER_MINUTE", 10)
	return &Config{
		Port:            appenv.String("PORT", "8080"),
		OpenAIAPIKey:    apiKey,
		OpenAIModel:     appenv.String("OPENAI_MODEL", "gpt-4o-realtime-preview"),
		AllowedOrigins:  defaultOrigins(),
		RateLimitRPS:    appenv.Float64("RATE_LIMIT_RPS", float64(rateLimitPerMinute)/60.0),
		RateLimitBurst:  appenv.Int("RATE_LIMIT_BURST", rateLimitPerMinute),
		TurnServersJSON: appenv.String("TURN_SERVERS_JSON", ""),
	}, nil
}

func defaultOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	frontendURL := appenv.String("FRONTEND_URL", "https://cleanapp.io")
	origins := []string{frontendURL, "http://localhost:3000", "http://localhost:3001"}
	if strings.Contains(frontendURL, "://cleanapp.io") {
		origins = append(origins, strings.Replace(frontendURL, "://cleanapp.io", "://www.cleanapp.io", 1))
	}
	return origins
}

// GetTurnServers parses the TURN_SERVERS_JSON environment variable
func (c *Config) GetTurnServers() []TurnServer {
	if c.TurnServersJSON == "" {
		return nil
	}

	var servers []TurnServer
	if err := json.Unmarshal([]byte(c.TurnServersJSON), &servers); err != nil {
		return nil
	}
	return servers
}
