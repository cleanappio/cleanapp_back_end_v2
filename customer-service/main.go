package main

import (
	"cleanapp-common/serverx"
	"database/sql"
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
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("ERROR: Failed to load config: %v", err)
	}

	db, err := database.OpenDB(cfg)
	if err != nil {
		log.Fatalf("ERROR: Failed to connect to database: %v", err)
	}
	defer db.Close()

	if cfg.RunDBMigrations {
		log.Println("Runtime DB migrations are disabled at service boot. Run customer-service/cmd/migrate instead.")
	}

	stripeClient := stripe.NewClient(cfg)
	service := database.NewCustomerService(db, stripeClient, cfg.AuthServiceURL)

	router := setupRouter(service, stripeClient, cfg, db)

	log.Printf("INFO: Server starting on port %s", cfg.Port)
	if err := serverx.New(":"+cfg.Port, router).ListenAndServe(); err != nil {
		log.Fatalf("ERROR: Failed to start server: %v", err)
	}
}

func setupRouter(service *database.CustomerService, stripeClient *stripe.Client, cfg *config.Config, db *sql.DB) *gin.Engine {
	router := gin.Default()
	router.SetTrustedProxies(cfg.TrustedProxies)
	router.Use(middleware.CORSMiddleware(cfg.AllowedOrigins))
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.RateLimitMiddleware(cfg.RateLimitRPS, cfg.RateLimitBurst))

	h := handlers.NewHandlers(service, stripeClient, cfg)

	router.GET("/health", h.RootHealthCheck)
	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("customer-service"))
	})

	public := router.Group("/api/v3")
	{
		public.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("customer-service"))
		})
		auth := public.Group("/auth")
		{
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.RefreshToken)
		}
		public.POST("/customers", h.CreateCustomer)
		public.GET("/users/exists", h.CheckUserExists)
		public.GET("/areas", h.GetAreas)
		public.GET("/prices", h.GetPrices)
		public.GET("/health", h.HealthCheck)
	}

	protected := router.Group("/api/v3")
	protected.Use(middleware.AuthMiddleware(cfg, db))
	{
		protected.POST("/auth/logout", h.Logout)
		protected.GET("/customers/me", h.GetCustomer)
		protected.PUT("/customers/me", h.UpdateCustomer)
		protected.DELETE("/customers/me", h.DeleteCustomer)
		protected.GET("/customers/me/brands", h.GetCustomerBrands)
		protected.POST("/customers/me/brands", h.AddCustomerBrands)
		protected.PUT("/customers/me/brands", h.UpdateCustomerBrands)
		protected.DELETE("/customers/me/brands", h.RemoveCustomerBrands)
		protected.GET("/customers/me/areas", h.GetCustomerAreas)
		protected.POST("/customers/me/areas", h.AddCustomerAreas)
		protected.PUT("/customers/me/areas", h.UpdateCustomerAreas)
		protected.DELETE("/customers/me/areas", h.DeleteCustomerAreas)
		protected.POST("/subscriptions", h.CreateSubscription)
		protected.GET("/subscriptions/me", h.GetSubscription)
		protected.PUT("/subscriptions/me", h.UpdateSubscription)
		protected.DELETE("/subscriptions/me", h.CancelSubscription)
		protected.POST("/subscriptions/me/reactivate", h.ReactivateSubscription)
		billing := protected.Group("/billing")
		{
			billing.GET("/history", h.GetBillingHistory)
			billing.GET("/invoices/:id", h.DownloadInvoice)
		}
		protected.GET("/payment-methods", h.GetPaymentMethods)
		protected.POST("/payment-methods", h.AddPaymentMethod)
		protected.PUT("/payment-methods/:id", h.UpdatePaymentMethod)
		protected.DELETE("/payment-methods/:id", h.DeletePaymentMethod)
	}

	webhooks := router.Group("/api/v3/webhooks")
	{
		webhooks.POST("/payment", h.ProcessPayment)
	}

	return router
}
