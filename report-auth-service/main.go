package main

import (
	"database/sql"
	"fmt"
	"log"

	"report-auth-service/config"
	"report-auth-service/database"
	"report-auth-service/handlers"
	"report-auth-service/middleware"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Database connection
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	log.Println("Initializing database schema...")
	if err := database.InitializeSchema(db); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize service
	service := database.NewReportAuthService(db)

	// Setup Gin router
	router := setupRouter(service, cfg)

	// Start server
	log.Printf("Report auth service starting on port %s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func setupDatabase(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/cleanapp?parseTime=true&multiStatements=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("ERROR: Failed to open database connection: %v", err)
		return nil, err
	}

	// Test connection
	if err := db.Ping(); err != nil {
		log.Printf("ERROR: Failed to ping database: %v", err)
		return nil, err
	}

	return db, nil
}

func setupRouter(service *database.ReportAuthService, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Set trusted proxies from config
	router.SetTrustedProxies(cfg.TrustedProxies)

	// Apply global middleware
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware())

	// Initialize handlers
	h := handlers.NewHandlers(service)

	// Root level health check (not under /api/v3)
	router.GET("/health", h.RootHealthCheck)

	// Public API routes
	api := router.Group("/api/v3")
	{
		// Health check
		api.GET("/health", h.HealthCheck)
	}

	// Protected API routes
	protected := router.Group("/api/v3")
	protected.Use(middleware.AuthMiddleware(cfg.AuthServiceURL))
	{
		// Report authorization (requires authentication)
		protected.POST("/reports/authorization", h.CheckReportAuthorization)
	}

	return router
}
