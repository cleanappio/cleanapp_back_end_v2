package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"brand-dashboard/config"
	"brand-dashboard/handlers"
	"brand-dashboard/middleware"
	"brand-dashboard/services"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found, using system environment variables")
	}

	// Load configuration
	cfg := config.Load()

	// Initialize brand service
	brandService := services.NewBrandService(cfg.BrandNames)
	log.Printf("INFO: Brand dashboard configured with brands: %v", cfg.BrandNames)

	// Initialize database service
	databaseService, err := services.NewDatabaseService(brandService)
	if err != nil {
		log.Fatalf("Failed to initialize database service: %v", err)
	}
	defer databaseService.Close()

	// Initialize WebSocket service
	websocketHub := services.NewWebSocketHub()

	// Start WebSocket service
	go websocketHub.Start()
	defer websocketHub.Stop()

	// Initialize handlers
	brandHandler := handlers.NewBrandHandler(brandService, databaseService)
	websocketHandler := handlers.NewWebSocketHandler(websocketHub)

	r := gin.Default()

	// CORS middleware for Gin
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})

	// Health endpoint (public)
	r.GET("/health", brandHandler.HealthHandler)

	// Protected routes
	protected := r.Group("/")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		protected.GET("/brands", brandHandler.BrandsHandler)
		protected.GET("/reports", brandHandler.ReportsHandler)
		protected.GET("/ws/brand-reports", websocketHandler.ListenBrandReports)
		protected.GET("/ws/health", websocketHandler.HealthCheck)
	}

	log.Printf("Starting Brand Dashboard service on %s:%s", cfg.Host, cfg.Port)
	log.Printf("Configured brands: %v", cfg.BrandNames)
	r.Run(cfg.Host + ":" + cfg.Port)
}
