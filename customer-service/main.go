package main

import (
	"cleanapp-common/serverx"
	"log"

	"customer-service/config"
	"customer-service/database"
	"customer-service/handlers"
	"customer-service/middleware"
	"customer-service/utils/stripe"
	"customer-service/version"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("ERROR: Failed to load config: %v", err)
	}

	// Database connection
	db, err := database.OpenDB(cfg)
	if err != nil {
		log.Fatalf("ERROR: Failed to connect to database: %v", err)
	}
	defer db.Close()

	if cfg.RunDBMigrations {
		log.Println("Runtime DB migrations are disabled at service boot. Run customer-service/cmd/migrate instead.")
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(cfg)

	// Initialize service with Stripe client
	service := database.NewCustomerService(db, stripeClient, cfg.AuthServiceURL)

	// Setup Gin router
	router := setupRouter(service, stripeClient, cfg)

	// Start server
	log.Printf("INFO: Server starting on port %s", cfg.Port)
	if err := serverx.New(":"+cfg.Port, router).ListenAndServe(); err != nil {
		log.Fatalf("ERROR: Failed to start server: %v", err)
	}
}

func setupRouter(service *database.CustomerService, stripeClient *stripe.Client, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Set trusted proxies from config
	router.SetTrustedProxies(cfg.TrustedProxies)

	// Apply global middleware
	router.Use(middleware.CORSMiddleware(cfg.AllowedOrigins))
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware(cfg.RateLimitRPS, cfg.RateLimitBurst))

	// Initialize handlers with Stripe client and config
	h := handlers.NewHandlers(service, stripeClient, cfg)

	// Root level health check (not under /api/v3)
	router.GET("/health", h.RootHealthCheck)
	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("customer-service"))
	})

	// Public routes
	public := router.Group("/api/v3")
	{
		public.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("customer-service"))
		})

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

		// Customer areas routes
		protected.GET("/customers/me/areas", h.GetCustomerAreas)
		protected.POST("/customers/me/areas", h.AddCustomerAreas)
		protected.PUT("/customers/me/areas", h.UpdateCustomerAreas)
		protected.DELETE("/customers/me/areas", h.DeleteCustomerAreas)

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
