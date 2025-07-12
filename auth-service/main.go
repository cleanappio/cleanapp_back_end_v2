package main

import (
	"database/sql"
	"fmt"
	"log"

	"auth-service/config"
	"auth-service/database"
	"auth-service/handlers"
	"auth-service/middleware"
	"auth-service/utils/encryption"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Database connection
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Initialize database schema
	log.Println("Initializing database schema and running migrations...")
	if err := database.InitializeSchema(db); err != nil {
		log.Fatal("Failed to initialize database schema:", err)
	}

	// Initialize encryptor
	encryptor, err := encryption.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal("Failed to initialize encryptor:", err)
	}

	// Initialize service
	service := database.NewAuthService(db, encryptor, cfg.JWTSecret)

	// Setup Gin router
	router := setupRouter(service, cfg)

	// Start server
	log.Printf("Auth service starting on port %s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func setupDatabase(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/cleanapp?parseTime=true&multiStatements=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func setupRouter(service *database.AuthService, cfg *config.Config) *gin.Engine {
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

	// Public routes
	public := router.Group("/api/v3")
	{
		// Authentication routes
		auth := public.Group("/auth")
		{
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.RefreshToken)
			auth.POST("/register", h.CreateUser)
		}

		// User existence check
		public.GET("/users/exists", h.CheckUserExists)

		// Token validation (for other services)
		public.POST("/validate-token", h.ValidateToken)

		// API health check
		public.GET("/health", h.HealthCheck)
	}

	// Protected routes
	protected := router.Group("/api/v3")
	protected.Use(middleware.AuthMiddleware(service))
	{
		// Authentication
		protected.POST("/auth/logout", h.Logout)

		// User routes
		protected.GET("/users/me", h.GetUser)
		protected.PUT("/users/me", h.UpdateUser)
		protected.DELETE("/users/me", h.DeleteUser)
	}

	return router
}
