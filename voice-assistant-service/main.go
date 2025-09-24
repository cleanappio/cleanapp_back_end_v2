package main

import (
	"strconv"
	"voice-assistant-service/config"
	"voice-assistant-service/handlers"
	"voice-assistant-service/openai"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
    EndPointHealth     = "/health"
    EndPointAssistant  = "/api/assistant"
)

func main() {
    // Load configuration
    cfg := config.Load()
    
    if cfg.OpenAIAPIKey == "" {
        log.Fatal("OPENAI_API_KEY environment variable is required")
    }
    
    log.Info("Starting the voice assistant service...")
    
    // Initialize OpenAI client
    openaiClient := openai.NewClient(cfg)
    
    // Initialize handlers
    voiceHandler := handlers.NewVoiceHandler(openaiClient)
    
    // Setup router
    router := gin.Default()
    
    // Add CORS middleware
    router.Use(func(c *gin.Context) {
        c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
        c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
        c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
        c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
        
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        
        c.Next()
    })
    
    // Register endpoints
    router.GET(EndPointHealth, voiceHandler.HealthCheck)
    router.POST(EndPointAssistant, voiceHandler.Assistant)
    
    // Get server port from config
    serverPort, err := strconv.Atoi(cfg.Port)
    if err != nil {
        log.Fatalf("Invalid PORT configuration: %v", err)
    }
    
    // Start server
    log.Infof("Voice assistant service starting on port %d", serverPort)
    if err := router.Run(":" + cfg.Port); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
}