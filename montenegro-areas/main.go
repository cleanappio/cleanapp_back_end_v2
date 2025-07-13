package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"montenegro-areas/config"
	"montenegro-areas/handlers"
	"montenegro-areas/middleware"
	"montenegro-areas/services"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found, using system environment variables")
	}

	// Load configuration
	cfg := config.Load()

	// Initialize areas service
	areasService := services.NewAreasService()

	// Load areas data
	log.Println("Loading Montenegro areas data...")
	if err := areasService.LoadAreas(); err != nil {
		log.Fatalf("Failed to load areas data: %v", err)
	}
	log.Println("Areas data loaded successfully")

	// Initialize database service
	databaseService, err := services.NewDatabaseService(areasService)
	if err != nil {
		log.Fatalf("Failed to initialize database service: %v", err)
	}
	defer databaseService.Close()

	// Initialize WebSocket service
	websocketService, err := services.NewWebSocketService(databaseService, areasService)
	if err != nil {
		log.Fatalf("Failed to initialize WebSocket service: %v", err)
	}

	// Start WebSocket service
	if err := websocketService.Start(); err != nil {
		log.Fatalf("Failed to start WebSocket service: %v", err)
	}
	defer websocketService.Stop()

	// Initialize handlers
	areasHandler := handlers.NewAreasHandler(areasService, databaseService)
	websocketHandler := handlers.NewWebSocketHandler(websocketService.GetHub())

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
	r.GET("/health", areasHandler.HealthHandler)

	// Protected routes
	protected := r.Group("/")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		protected.GET("/areas", areasHandler.AreasByAdminLevelHandler)
		protected.GET("/admin-levels", areasHandler.AvailableAdminLevelsHandler)
		protected.GET("/reports", areasHandler.ReportsHandler)
		protected.GET("/reports_aggr", areasHandler.ReportsAggrHandler)
		protected.GET("/ws/montenegro-reports", websocketHandler.ListenMontenegroReports)
		protected.GET("/ws/health", websocketHandler.HealthCheck)
	}

	log.Printf("Starting Montenegro Areas service on %s:%s", cfg.Host, cfg.Port)
	r.Run(cfg.Host + ":" + cfg.Port)
}
