package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"email-service/config"
	"email-service/handlers"
	"email-service/service"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Create email service
	emailService, err := service.NewEmailService(cfg)
	if err != nil {
		log.Fatal("Failed to create email service:", err)
	}
	defer emailService.Close()

	// Create HTTP handler
	handler := handlers.NewEmailServiceHandler(emailService)

	// Create Gin router
	router := gin.Default()

	// API v3 routes
	apiV3 := router.Group("/api/v3")
	{
		apiV3.POST("/optout", handler.HandleOptOut)
	}

	// Opt-out link route (for email links)
	router.GET("/opt-out", handler.HandleOptOutLink)

	// Health check
	router.GET("/health", handler.HandleHealth)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("HTTP server starting on port %s", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Start polling for reports in a goroutine
	go func() {
		pollInterval := cfg.GetPollInterval()
		log.Printf("Email service started. Polling every %v", pollInterval)
		for {
			iterStart := time.Now()
			log.Printf("Polling tick started at %s", iterStart.Format(time.RFC3339))
			if err := emailService.ProcessReports(); err != nil {
				log.Printf("Error processing reports: %v", err)
			}
			log.Printf("Polling tick finished in %s; sleeping %v", time.Since(iterStart), pollInterval)
			time.Sleep(pollInterval)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Server is shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
