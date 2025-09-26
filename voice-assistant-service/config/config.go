package config

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
    // Server configuration
    Port string
    
    // OpenAI configuration
    OpenAIAPIKey string
    OpenAIModel  string
    
    // CORS configuration
    AllowedOrigins string
    
    // Rate limiting
    RateLimitPerMinute int
    
    // TURN servers (optional)
    TurnServersJSON string
}

type TurnServer struct {
    URLs       []string `json:"urls"`
    Username   string   `json:"username,omitempty"`
    Credential string   `json:"credential,omitempty"`
}

func Load() *Config {
    return &Config{
        Port:              getEnv("PORT", "8080"),
        OpenAIAPIKey:      getEnv("OPENAI_API_KEY", ""),
        OpenAIModel:       getEnv("OPENAI_MODEL", "gpt-4o-realtime-preview"),
        AllowedOrigins:    getEnv("ALLOWED_ORIGINS", "*"),
        RateLimitPerMinute: getIntEnv("RATE_LIMIT_PER_MINUTE", 10),
        TurnServersJSON:   getEnv("TURN_SERVERS_JSON", ""),
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        if intValue, err := strconv.Atoi(value); err == nil {
            return intValue
        }
    }
    return defaultValue
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