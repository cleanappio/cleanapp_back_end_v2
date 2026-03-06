package main

import (
	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report_processor/config"
	"report_processor/database"
	"report_processor/handlers"
	"report_processor/middleware"
	"report_processor/rabbitmq"
	"report_processor/version"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to create database connection:", err)
	}
	defer db.Close()

	if cfg.RunDBMigrations {
		log.Printf("Runtime DB migrations are disabled at service boot. Run report-processor/cmd/migrate instead.")
	}

	var rabbitmqPublisher *rabbitmq.Publisher
	amqpURL := cfg.GetAMQPURL()
	publisher, err := rabbitmq.NewPublisher(amqpURL, cfg.RabbitMQExchange, cfg.RabbitMQRawReportRoutingKey)
	if err != nil {
		log.Printf("Warning: Failed to initialize RabbitMQ publisher: %v", err)
		log.Printf("Tag processing via RabbitMQ will be unavailable. Continuing without RabbitMQ...")
	} else {
		rabbitmqPublisher = publisher
		log.Printf("RabbitMQ publisher initialized: exchange=%s, routing_key=%s", cfg.RabbitMQExchange, cfg.RabbitMQRawReportRoutingKey)
	}

	h := handlers.NewHandlers(db, cfg, rabbitmqPublisher)
	router := setupRouter(cfg, h, db.GetDB())
	srv := serverx.New(":"+cfg.Port, router)

	go func() {
		log.Printf("Starting HTTP server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start HTTP server:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	if rabbitmqPublisher != nil {
		if err := rabbitmqPublisher.Close(); err != nil {
			log.Printf("Failed to close RabbitMQ publisher: %v", err)
		} else {
			log.Println("RabbitMQ publisher closed successfully")
		}
	}

	log.Println("Server exited")
}

func setupRouter(cfg *config.Config, h *handlers.Handlers, db *sql.DB) *gin.Engine {
	router := gin.Default()

	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}}))

	api := router.Group("/api/v3")
	{
		api.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("report-processor"))
		})

		protected := api.Group("/reports")
		protected.Use(middleware.AuthMiddleware(cfg, db))
		{
			protected.POST("/mark_resolved", h.MarkResolved)
		}

		api.GET("/reports/status", h.GetReportStatus)
		api.GET("/reports/status/count", h.GetReportStatusCount)
		api.POST("/match_report", h.MatchReport)
		api.GET("/responses/get", h.GetResponse)
		api.GET("/responses/by_status", h.GetResponsesByStatus)
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"service": "report-processor",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})
	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("report-processor"))
	})

	return router
}
