package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"auth-service/config"
	"auth-service/database"
	"auth-service/handlers"
	"auth-service/middleware"
	"auth-service/utils/email"
	"auth-service/utils/encryption"
	"auth-service/version"

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
	log.Println("Initializing database schema and running migrations...")
	if err := database.InitializeSchema(db); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize encryptor
	encryptor, err := encryption.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("Failed to initialize encryptor: %v", err)
	}

	// Initialize service
	service := database.NewAuthService(db, encryptor, cfg.JWTSecret)

	// Setup Gin router
	router := setupRouter(service, cfg)

	// Start server
	log.Printf("Auth service starting on port %s", cfg.Port)
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

	applyDBPoolSettings(db)

	// Test connection with exponential backoff retry (bounded)
	maxWaitSec := envInt([]string{"AUTH_SERVICE_DB_PING_MAX_WAIT_SEC", "DB_PING_MAX_WAIT_SEC"}, 60)
	deadline := time.Now().Add(time.Duration(maxWaitSec) * time.Second)
	var waitInterval time.Duration = 1 * time.Second
	for {
		pingErr := db.Ping()
		if pingErr == nil {
			break // Connection successful
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database ping timeout after %ds: %w", maxWaitSec, pingErr)
		}
		log.Printf("Database connection failed, retrying in %v: %v", waitInterval, pingErr)
		time.Sleep(waitInterval)
		waitInterval *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, ...
		if waitInterval > 30*time.Second {
			waitInterval = 30 * time.Second
		}
	}

	return db, nil
}

func applyDBPoolSettings(db *sql.DB) {
	maxOpen := envInt([]string{"AUTH_SERVICE_DB_MAX_OPEN_CONNS", "DB_MAX_OPEN_CONNS"}, 20)
	maxIdle := envInt([]string{"AUTH_SERVICE_DB_MAX_IDLE_CONNS", "DB_MAX_IDLE_CONNS"}, 10)
	maxLifetimeMin := envInt([]string{"AUTH_SERVICE_DB_CONN_MAX_LIFETIME_MIN", "DB_CONN_MAX_LIFETIME_MIN"}, 5)

	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	if maxLifetimeMin > 0 {
		db.SetConnMaxLifetime(time.Duration(maxLifetimeMin) * time.Minute)
	}
}

func envInt(keys []string, def int) int {
	for _, key := range keys {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			return n
		}
	}
	return def
}

func setupRouter(service *database.AuthService, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Set trusted proxies from config
	router.SetTrustedProxies(cfg.TrustedProxies)

	// Apply global middleware
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware())

	// Initialize email sender (if SendGrid is configured)
	var emailSender *email.Sender
	if cfg.SendGridAPIKey != "" {
		emailSender = email.NewSender(cfg.SendGridAPIKey, cfg.SendGridFromName, cfg.SendGridFromEmail)
		log.Println("Email sender initialized with SendGrid")
	} else {
		log.Println("WARNING: SendGrid API key not configured, password reset emails will not be sent")
	}

	// Initialize handlers
	h := handlers.NewHandlers(service, emailSender, cfg.FrontendURL)

	// Root level health check (not under /api/v3)
	router.GET("/health", h.RootHealthCheck)
	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("auth-service"))
	})

	// Public routes
	public := router.Group("/api/v3")
	{
		public.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("auth-service"))
		})

		// Authentication routes
		auth := public.Group("/auth")
		{
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.RefreshToken)
			auth.POST("/register", h.CreateUser)
			auth.POST("/forgot-password", h.ForgotPassword)
			auth.POST("/reset-password", h.ResetPassword)
		}

		// User existence check
		public.GET("/users/exists", h.CheckUserExists)

		// Get user by ID (for internal service calls)
		public.GET("/users/:id", h.GetUserByID)

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
