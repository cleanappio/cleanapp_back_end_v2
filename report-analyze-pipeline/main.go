package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/handlers"
	"report-analyze-pipeline/rabbitmq"
	"report-analyze-pipeline/service"
	"report-analyze-pipeline/version"

	"github.com/gin-gonic/gin"
)

// Global RabbitMQ subscriber instance
var reportSubscriber *rabbitmq.Subscriber

// initializeReportSubscriber initializes the RabbitMQ subscriber for reports
func initializeReportSubscriber(cfg *config.Config, analysisService *service.Service) error {
	// Get RabbitMQ configuration from config
	rabbitMQConfig := cfg.RabbitMQ
	amqpURL := rabbitMQConfig.GetAMQPURL()

	subscriber, err := rabbitmq.NewSubscriber(amqpURL, rabbitMQConfig.Exchange, rabbitMQConfig.Queue, rabbitMQConfig.PrefetchCount)
	if err != nil {
		return fmt.Errorf("failed to initialize RabbitMQ subscriber: %w", err)
	}

	// Define callback for report processing
	callbacks := map[string]rabbitmq.CallbackFunc{
		rabbitMQConfig.RawReportRoutingKey: func(msg *rabbitmq.Message) error {
			return processReportMessage(msg, analysisService)
		},
	}

	// Start consuming messages
	err = subscriber.Start(callbacks)
	if err != nil {
		subscriber.Close()
		return fmt.Errorf("failed to start RabbitMQ subscriber: %w", err)
	}

	reportSubscriber = subscriber
	log.Printf("RabbitMQ subscriber initialized: exchange=%s, queue=%s, routing_key=%s",
		rabbitMQConfig.Exchange, rabbitMQConfig.Queue, rabbitMQConfig.RawReportRoutingKey)
	return nil
}

// closeReportSubscriber closes the RabbitMQ subscriber
func closeReportSubscriber() {
	if reportSubscriber != nil {
		err := reportSubscriber.Close()
		if err != nil {
			log.Printf("Failed to close RabbitMQ subscriber: %v", err)
		} else {
			log.Println("RabbitMQ subscriber closed successfully")
		}
	}
}

// processReportMessage processes a report message from RabbitMQ
func processReportMessage(msg *rabbitmq.Message, analysisService *service.Service) error {
	var report database.Report
	err := msg.UnmarshalTo(&report)
	if err != nil {
		return rabbitmq.Permanent(fmt.Errorf("failed to unmarshal report message: %w", err))
	}

	log.Printf("Received report for analysis from RabbitMQ: seq=%d, image_size=%d bytes", report.Seq, len(report.Image))

	// Analyze the report using the same logic as the HTTP handler
	// The AnalyzeReport method will fetch the complete report data (including image) from the database
	return analysisService.AnalyzeReport(&report)
}

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	switch cfg.LLMProvider {
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			log.Fatal("GEMINI_API_KEY environment variable is required when ANALYZER_LLM_PROVIDER=gemini")
		}
	default: // openai
		if cfg.OpenAIAPIKey == "" {
			log.Fatal("OPENAI_API_KEY environment variable is required when ANALYZER_LLM_PROVIDER=openai")
		}
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

	// Initialize RabbitMQ subscriber for reports
	err = initializeReportSubscriber(cfg, analysisService)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ subscriber: %v", err)
	}

	// Ensure cleanup on exit
	defer closeReportSubscriber()

	// Initialize handlers
	handlers := handlers.NewHandlers(db, analysisService)

	// Setup HTTP server
	router := gin.Default()

	// API routes
	api := router.Group("/api/v3")
	{
		api.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("report-analyze-pipeline"))
		})
		api.GET("/health", handlers.HealthCheck)
		api.GET("/status", handlers.GetAnalysisStatus)
		api.GET("/analysis/:seq", handlers.GetAnalysisBySeq)
		api.GET("/stats", handlers.GetAnalysisStats)
	}

	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("report-analyze-pipeline"))
	})

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

	// Start background enrichment job for external digital reports
	enrichmentTicker := time.NewTicker(10 * time.Second)
	enrichmentDone := make(chan bool)
	go func() {
		// Run once immediately on startup
		analysisService.EnrichExternalDigitalReports()
		for {
			select {
			case <-enrichmentTicker.C:
				analysisService.EnrichExternalDigitalReports()
			case <-enrichmentDone:
				return
			}
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop the enrichment ticker
	enrichmentTicker.Stop()
	close(enrichmentDone)

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
