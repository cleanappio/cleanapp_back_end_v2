package main

import (
	"database/sql"
	"fmt"
	"log"

	"customer-service/config"
	"customer-service/database"
	"customer-service/handlers"
	"customer-service/middleware"
	"customer-service/utils/stripe"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Database connection
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatalf("ERROR: Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	log.Println("Initializing database schema and running migrations...")
	if err := database.InitializeSchema(db); err != nil {
		log.Fatalf("ERROR: Failed to initialize database schema: %v", err)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(cfg)

	// Initialize service with Stripe client
	service := database.NewCustomerService(db, stripeClient, cfg.AuthServiceURL)

	// Setup Gin router
	router := setupRouter(service, stripeClient, cfg)

	// Start server
	log.Printf("INFO: Server starting on port %s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("ERROR: Failed to start server: %v", err)
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

func setupRouter(service *database.CustomerService, stripeClient *stripe.Client, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Set trusted proxies from config
	router.SetTrustedProxies(cfg.TrustedProxies)

	// Apply global middleware
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware())

	// Initialize handlers with Stripe client and config
	h := handlers.NewHandlers(service, stripeClient, cfg)

	// Root level health check (not under /api/v3)
	router.GET("/health", h.RootHealthCheck)

	// Public routes
	public := router.Group("/api/v3")
	{
		// Authentication routes (proxied to auth-service)
		auth := public.Group("/auth")
		{
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.RefreshToken)
		}

		// Customer registration (proxied to auth-service)
		public.POST("/customers", h.CreateCustomer)

		// User existence check (proxied to auth-service)
		public.GET("/users/exists", h.CheckUserExists)

		// Public data
		public.GET("/areas", h.GetAreas)

		// Prices and plans
		public.GET("/prices", h.GetPrices)

		// API health check
		public.GET("/health", h.HealthCheck)
	}

	// Protected routes
	protected := router.Group("/api/v3")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		// Authentication (proxied to auth-service)
		protected.POST("/auth/logout", h.Logout)

		// Customer routes
		protected.GET("/customers/me", h.GetCustomer)
		protected.PUT("/customers/me", h.UpdateCustomer)
		protected.DELETE("/customers/me", h.DeleteCustomer)

		// Customer brands routes
		protected.GET("/customers/me/brands", h.GetCustomerBrands)
		protected.POST("/customers/me/brands", h.AddCustomerBrands)
		protected.PUT("/customers/me/brands", h.UpdateCustomerBrands)
		protected.DELETE("/customers/me/brands", h.RemoveCustomerBrands)

		// Subscription routes
		protected.POST("/subscriptions", h.CreateSubscription)
		protected.GET("/subscriptions/me", h.GetSubscription)
		protected.PUT("/subscriptions/me", h.UpdateSubscription)
		protected.DELETE("/subscriptions/me", h.CancelSubscription)
		protected.POST("/subscriptions/me/reactivate", h.ReactivateSubscription)

		// Billing routes
		billing := protected.Group("/billing")
		{
			billing.GET("/history", h.GetBillingHistory)
			billing.GET("/invoices/:id", h.DownloadInvoice)
		}

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
