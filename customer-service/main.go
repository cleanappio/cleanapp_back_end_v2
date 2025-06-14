package main

import (
	"database/sql"
	"fmt"
	"log"

	"customer-service/config"
	"customer-service/database"
	"customer-service/handlers"
	"customer-service/middleware"
	"customer-service/utils/encryption"
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
	service := database.NewCustomerService(db, encryptor, cfg.JWTSecret)

	// Setup Gin router
	router := setupRouter(service, cfg)

	// Start server
	log.Printf("Server starting on port %s", cfg.Port)
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

func setupRouter(service *database.CustomerService, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Set trusted proxies from config
	router.SetTrustedProxies(cfg.TrustedProxies)

	// Apply global middleware
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware())

	// Initialize handlers
	h := handlers.NewHandlers(service)

	// Public routes
	public := router.Group("/api/v3")
	{
		public.POST("/login", h.Login)
		public.POST("/customers", h.CreateCustomer)
		public.GET("/health", h.HealthCheck)
	}

	// Protected routes
	protected := router.Group("/api/v3")
	protected.Use(middleware.AuthMiddleware(service))
	{
		// Customer routes
		protected.GET("/customers/me", h.GetCustomer)
		protected.PUT("/customers/me", h.UpdateCustomer)
		protected.DELETE("/customers/me", h.DeleteCustomer)

		// Subscription routes
		protected.POST("/subscriptions", h.CreateSubscription)
		protected.GET("/subscriptions/me", h.GetSubscription)
		protected.PUT("/subscriptions/me", h.UpdateSubscription)
		protected.DELETE("/subscriptions/me", h.CancelSubscription)
		protected.GET("/billing-history", h.GetBillingHistory)

		// Payment routes
		protected.GET("/payment-methods", h.GetPaymentMethods)
		protected.POST("/payment-methods", h.AddPaymentMethod)
		protected.PUT("/payment-methods/:id", h.UpdatePaymentMethod)
		protected.DELETE("/payment-methods/:id", h.DeletePaymentMethod)
	}

	// Webhook routes (usually have different authentication)
	webhooks := router.Group("/api/v3/webhooks")
	{
		webhooks.POST("/payment", h.ProcessPayment)
	}

	return router
}
