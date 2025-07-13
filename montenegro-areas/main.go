package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
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

	router := mux.NewRouter()

	// Add CORS middleware
	router.Use(corsMiddleware)

	// Health endpoint (public)
	router.HandleFunc("/health", areasHandler.HealthHandler).Methods("GET")

	// Protected routes - require authentication
	protectedRouter := router.PathPrefix("/").Subrouter()
	protectedRouter.Use(middleware.AuthMiddleware(cfg))

	// Areas endpoints (protected)
	protectedRouter.HandleFunc("/areas", areasHandler.AreasByAdminLevelHandler).Methods("GET")
	protectedRouter.HandleFunc("/admin-levels", areasHandler.AvailableAdminLevelsHandler).Methods("GET")

	// Reports endpoints (protected)
	protectedRouter.HandleFunc("/reports", areasHandler.ReportsHandler).Methods("GET")
	protectedRouter.HandleFunc("/reports_aggr", areasHandler.ReportsAggrHandler).Methods("GET")

	// WebSocket endpoints (protected)
	protectedRouter.HandleFunc("/ws/montenegro-reports", websocketHandler.ListenMontenegroReports).Methods("GET")
	protectedRouter.HandleFunc("/ws/health", websocketHandler.HealthCheck).Methods("GET")

	log.Printf("Starting Montenegro Areas service on %s:%s", cfg.Host, cfg.Port)
	log.Fatal(http.ListenAndServe(cfg.Host+":"+cfg.Port, router))
}

// corsMiddleware handles CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}
