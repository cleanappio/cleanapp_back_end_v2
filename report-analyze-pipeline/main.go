package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/handlers"
	"report-analyze-pipeline/service"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	if cfg.OpenAIAPIKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Validate start point is set
	if cfg.SeqStartFrom <= 0 {
		log.Fatal("SEQ_START_FROM environment variable must be greater than 0")
	}

	// Initialize database
	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize service
	analysisService := service.NewService(cfg, db)

	// Initialize handlers
	handlers := handlers.NewHandlers(db)

	// Setup HTTP server
	router := gin.Default()

	// API routes
	api := router.Group("/api/v3")
	{
		api.GET("/health", handlers.HealthCheck)
		api.GET("/status", handlers.GetAnalysisStatus)
		api.GET("/analysis/:seq", handlers.GetAnalysisBySeq)
		api.GET("/stats", handlers.GetAnalysisStats)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Start the analysis service
	analysisService.Start()

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop the analysis service
	analysisService.Stop()

	// Create a deadline for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}
