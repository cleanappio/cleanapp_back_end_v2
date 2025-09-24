package config

import "os"

type Config struct {
    // Server configuration
    Port string
    
    // OpenAI configuration
    OpenAIAPIKey string
    OpenAIModel  string
}

func Load() *Config {
    return &Config{
        Port:         getEnv("PORT", "8080"),
        OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
        OpenAIModel:  getEnv("OPENAI_MODEL", "gpt-4o"),
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}