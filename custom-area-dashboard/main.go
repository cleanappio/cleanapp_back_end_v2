package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"custom-area-dashboard/config"
	"custom-area-dashboard/handlers"
	"custom-area-dashboard/middleware"
	"custom-area-dashboard/services"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found, using system environment variables")
	}

	// Load configuration
	cfg := config.Load()

	// Initialize database service
	databaseService, err := services.NewDatabaseService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database service: %v", err)
	}
	defer databaseService.Close()

	// Initialize handlers
	areasHandler := handlers.NewAreasHandler(databaseService, cfg)
	r := gin.Default()

	// CORS middleware for Gin
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// Health endpoint (public)
	r.GET("/health", areasHandler.HealthHandler)

	// Protected routes
	protected := r.Group("/")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		protected.GET("/areas", areasHandler.AreasHandler)
		protected.GET("/sub_areas", areasHandler.SubAreasHandler)
		protected.GET("/reports", areasHandler.ReportsHandler)
		protected.GET("/reports_aggr", areasHandler.ReportsAggrHandler)
	}

	log.Printf("Starting Custom Area Dashboard service on %s:%s", cfg.Host, cfg.Port)
	r.Run(cfg.Host + ":" + cfg.Port)
}
