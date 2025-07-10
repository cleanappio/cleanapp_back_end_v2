package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"

	"montenegro-areas/handlers"
	"montenegro-areas/services"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found, using system environment variables")
	}

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

	// Initialize handlers
	areasHandler := handlers.NewAreasHandler(areasService, databaseService)

	router := mux.NewRouter()

	// Health endpoint
	router.HandleFunc("/health", areasHandler.HealthHandler).Methods("GET")

	// Areas endpoints
	router.HandleFunc("/areas", areasHandler.AreasByAdminLevelHandler).Methods("GET")
	router.HandleFunc("/admin-levels", areasHandler.AvailableAdminLevelsHandler).Methods("GET")

	// Reports endpoint
	router.HandleFunc("/reports", areasHandler.ReportsHandler).Methods("GET")

	// Get port from environment variable or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Get host from environment variable or default to 0.0.0.0
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	log.Printf("Starting Montenegro Areas service on %s:%s", host, port)
	log.Fatal(http.ListenAndServe(host+":"+port, router))
}
