package main

import (
	"context"
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

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Create database connection
	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to create database connection:", err)
	}
	defer db.Close()

	// Create auth client
	authClient := database.NewAuthClient(cfg.AuthServiceURL)

	// Ensure report_status table exists
	if err := db.EnsureReportStatusTable(context.Background()); err != nil {
		log.Fatal("Failed to ensure report_status table:", err)
	}

	// Ensure responses table exists
	if err := db.EnsureResponsesTable(context.Background()); err != nil {
		log.Fatal("Failed to ensure responses table:", err)
	}

	// Create handlers
	h := handlers.NewHandlers(db, cfg)

	// Setup HTTP server
	router := setupRouter(cfg, h, authClient)

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start HTTP server:", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Create a deadline for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown the HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}

func setupRouter(cfg *config.Config, h *handlers.Handlers, authClient *database.AuthClient) *gin.Engine {
	router := gin.Default()

	// Add CORS middleware
	router.Use(middleware.CORSMiddleware())

	// Add security headers
	router.Use(middleware.SecurityHeaders())

	// API routes
	api := router.Group("/api/v3")
	{
		// Protected routes (require authentication)
		protected := api.Group("/reports")
		protected.Use(middleware.AuthMiddleware(cfg, authClient))
		{
			// Mark report as resolved endpoint
			protected.POST("/mark_resolved", h.MarkResolved)
		}

		// Public routes
		{
			// Get report status endpoint
			api.GET("/reports/status", h.GetReportStatus)

			// Get report status count endpoint
			api.GET("/reports/status/count", h.GetReportStatusCount)

			// Match report endpoint
			api.POST("/match_report", h.MatchReport)

			// Get response endpoint
			api.GET("/responses/get", h.GetResponse)

			// Get responses by status endpoint
			api.GET("/responses/by_status", h.GetResponsesByStatus)
		}
	}

	// Root health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"service": "report-processor",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	return router
}
