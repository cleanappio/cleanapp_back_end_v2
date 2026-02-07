package main

import (
	"areas-service/config"
	"areas-service/database"
	"areas-service/handlers"
	"areas-service/middleware"
	"areas-service/utils"
	"areas-service/version"
	"fmt"
	"strconv"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHealth             = "/health"
	EndPointCreateOrUpdateArea = "/create_or_update_area"
	EndPointGetAreas           = "/get_areas"
	EndPointUpdateConsent      = "/update_consent"
	EndPointGetAreasCount      = "/get_areas_count"
	EndPointDeleteArea         = "/delete_area"
)

func main() {
	// Load configuration
	cfg := config.Load()

	log.Info("Starting the areas service...")

	// Connect to database
	db, err := utils.DBConnect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	if err := database.InitSchema(db); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize services
	areasService := database.NewAreasService(db)

	// Initialize handlers
	areasHandler := handlers.NewAreasHandler(areasService)

	// Setup router
	router := gin.Default()
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

	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("areas-service"))
	})

	// Register health endpoint (outside API group)
	router.GET(EndPointHealth, areasHandler.HealthCheck)

	// Create API v3 router group
	apiV3 := router.Group("/api/v3")
	{
		apiV3.POST(EndPointCreateOrUpdateArea, middleware.AuthMiddleware(cfg), areasHandler.CreateOrUpdateArea)
		apiV3.GET(EndPointGetAreas, areasHandler.GetAreas)
		apiV3.POST(EndPointUpdateConsent, areasHandler.UpdateConsent)
		apiV3.GET(EndPointGetAreasCount, areasHandler.GetAreasCount)
		apiV3.DELETE(EndPointDeleteArea, middleware.AuthMiddleware(cfg), areasHandler.DeleteArea)
	}

	// Get server port from config
	serverPort, err := strconv.Atoi(cfg.Port)
	if err != nil {
		log.Fatalf("Invalid PORT configuration: %v", err)
	}

	// Start server
	log.Infof("Areas service starting on port %d", serverPort)
	if err := router.Run(fmt.Sprintf(":%d", serverPort)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
