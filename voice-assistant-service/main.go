package main

import (
	"strconv"
	"time"
	"voice-assistant-service/config"
	"voice-assistant-service/handlers"
	"voice-assistant-service/middleware"
	"voice-assistant-service/version"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHealth        = "/health"
	EndPointSession       = "/session"
	EndPointPrewarm       = "/session/prewarm"
	EndPointProxyOffer    = "/webrtc/proxy-offer"
)

func main() {
	// Load configuration
	cfg := config.Load()
	
	if cfg.OpenAIAPIKey == "" {
		log.Fatal("TRASHFORMER_OPENAI_API_KEY environment variable is required")
	}
	
	log.Info("Starting the voice assistant service...")
	
	// Initialize handlers
	sessionHandler := handlers.NewSessionHandler(cfg)
	webrtcHandler := handlers.NewWebRTCHandler()
	
	// Setup router
	router := gin.Default()
	
	// Add CORS middleware
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", cfg.AllowedOrigins)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})
	
	// Health check endpoint (no auth required)
	router.GET(EndPointHealth, func(c *gin.Context) {
		info := version.Get("voice-assistant-service")
		c.JSON(200, gin.H{
			"status":  "healthy",
			"service": info.Service,
			"version": info.Version,
			"git_sha": info.GitSHA,
			"build_time": info.BuildTime,
		})
	})

	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("voice-assistant-service"))
	})
	
	// Rate-limited endpoints (no auth required for mobile app compatibility)
	rateLimited := router.Group("/")
	rateLimited.Use(middleware.RateLimitMiddleware(cfg.RateLimitPerMinute, time.Minute))
	{
		// Session management
		rateLimited.POST(EndPointSession, sessionHandler.CreateEphemeralSession)
		rateLimited.POST(EndPointPrewarm, sessionHandler.PrewarmSession)
		
		// WebRTC proxy (optional)
		rateLimited.POST(EndPointProxyOffer, webrtcHandler.ProxyOffer)
	}
	
	// Get server port from config
	serverPort, err := strconv.Atoi(cfg.Port)
	if err != nil {
		log.Fatalf("Invalid PORT configuration: %v", err)
	}
	
	// Start server
	log.Infof("Voice assistant service starting on port %d", serverPort)
	log.Infof("Rate limit: %d requests per minute", cfg.RateLimitPerMinute)
	log.Infof("Allowed origins: %s", cfg.AllowedOrigins)
	
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
