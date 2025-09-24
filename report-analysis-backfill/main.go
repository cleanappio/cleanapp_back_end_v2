package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-analysis-backfill/config"
	"report-analysis-backfill/database"
	"report-analysis-backfill/handlers"
	"report-analysis-backfill/service"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	if cfg.ReportAnalysisURL == "" {
		log.Fatal("REPORT_ANALYSIS_URL environment variable is required")
	}

	log.Printf("Starting report analysis backfill service...")
	log.Printf("Configuration: PollInterval=%v, BatchSize=%d, ReportAnalysisURL=%s",
		cfg.PollInterval, cfg.BatchSize, cfg.ReportAnalysisURL)

	// Initialize database
	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize backfill service
	backfillService := service.NewBackfillService(cfg, db)

	// Initialize handlers
	handlers := handlers.NewHandlers(backfillService)

	// Setup HTTP server
	router := gin.Default()

	// API routes
	api := router.Group("/api/v1")
	{
		api.GET("/health", handlers.HealthCheck)
		api.GET("/status", handlers.GetStatus)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Start the backfill service
	backfillService.Start()

	// Start HTTP server in a goroutine
	go func() {
		log.Println("Starting HTTP server on port 8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop the backfill service
	backfillService.Stop()

	// Create a deadline for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}
