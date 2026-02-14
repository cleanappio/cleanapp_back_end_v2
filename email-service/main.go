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
	"email-service/version"

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

	// Load HTML templates for opt-out pages
	router.LoadHTMLGlob("templates/*")

	// API v3 routes
	apiV3 := router.Group("/api/v3")
	{
		apiV3.GET("/version", func(c *gin.Context) {
			c.JSON(200, version.Get("email-service"))
		})
		apiV3.POST("/optout", handler.HandleOptOut)
	}

	// Opt-out link route (for email links)
	router.GET("/opt-out", handler.HandleOptOutLink)

	// Health check
	router.GET("/health", handler.HandleHealth)
	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("email-service"))
	})

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
	// Uses aggregate notifications: groups reports by brand and sends one email per brand
	go func() {
		pollInterval := cfg.GetPollInterval()
		log.Printf("Email service started (aggregate mode). Polling every %v", pollInterval)
		for {
			iterStart := time.Now()
			log.Printf("Aggregate notification tick started at %s", iterStart.Format(time.RFC3339))
			if err := emailService.ProcessBrandNotifications(); err != nil {
				log.Printf("Error processing brand notifications: %v", err)
			}
			log.Printf("Aggregate notification tick finished in %s; sleeping %v", time.Since(iterStart), pollInterval)
			time.Sleep(pollInterval)
		}
	}()

	// Brandless physical pipeline (bounded).
	// This fills the gap where physical reports have recipients but no brand_name, so
	// they don't get picked up by aggregate brand grouping.
	if cfg.BrandlessPhysicalEnabled {
		go func() {
			interval := cfg.GetBrandlessPhysicalPollInterval()
			log.Printf("Email service started (brandless physical). Polling every %v (limit=%d)", interval, cfg.BrandlessPhysicalBatchLimit)
			for {
				iterStart := time.Now()
				log.Printf("Brandless physical tick started at %s", iterStart.Format(time.RFC3339))
				if err := emailService.ProcessBrandlessPhysicalReports(cfg.BrandlessPhysicalBatchLimit); err != nil {
					log.Printf("Error processing brandless physical reports: %v", err)
				}
				log.Printf("Brandless physical tick finished in %s; sleeping %v", time.Since(iterStart), interval)
				time.Sleep(interval)
			}
		}()
	} else {
		log.Printf("Brandless physical pipeline disabled (EMAIL_BRANDLESS_PHYSICAL_ENABLED=false)")
	}

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
