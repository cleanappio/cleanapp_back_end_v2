package main

import (
	"areas-service/database"
	"areas-service/handlers"
	"areas-service/utils"
	"flag"
	"fmt"
	"time"

	"github.com/apex/log"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	EndPointCreateOrUpdateArea = "/create_or_update_area"
	EndPointGetAreas           = "/get_areas"
	EndPointUpdateConsent      = "/update_consent"
	EndPointGetAreasCount      = "/get_areas_count"
)

var (
	serverPort = flag.Int("port", 8081, "The port used by the areas service.")
)

func main() {
	flag.Parse()

	log.Info("Starting the areas service...")

	// Connect to database
	db, err := utils.DBConnect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize services
	areasService := database.NewAreasService(db)

	// Initialize handlers
	areasHandler := handlers.NewAreasHandler(areasService)

	// Setup router
	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Register routes
	router.POST(EndPointCreateOrUpdateArea, areasHandler.CreateOrUpdateArea)
	router.GET(EndPointGetAreas, areasHandler.GetAreas)
	router.POST(EndPointUpdateConsent, areasHandler.UpdateConsent)
	router.GET(EndPointGetAreasCount, areasHandler.GetAreasCount)

	// Start server
	log.Infof("Areas service starting on port %d", *serverPort)
	if err := router.Run(fmt.Sprintf(":%d", *serverPort)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
