package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-listener/config"
	"report-listener/service"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Set log level
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create service
	svc, err := service.NewService(cfg)
	if err != nil {
		log.Fatal("Failed to create service:", err)
	}

	// Start service
	if err := svc.Start(); err != nil {
		log.Fatal("Failed to start service:", err)
	}

	// Setup HTTP server
	router := setupRouter(svc)

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

	// Stop the service
	if err := svc.Stop(); err != nil {
		log.Printf("Error stopping service: %v", err)
	}

	// Shutdown the HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}

func setupRouter(svc *service.Service) *gin.Engine {
	router := gin.Default()

	// Add gzip compression middleware
	router.Use(gzip.Gzip(gzip.DefaultCompression))

	// Add logging middleware to show compression usage
	router.Use(func(c *gin.Context) {
		// Log the request
		log.Printf("Request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Process the request
		c.Next()

		// Log response details
		contentLength := c.Writer.Header().Get("Content-Length")
		contentEncoding := c.Writer.Header().Get("Content-Encoding")
		log.Printf("Response: %d, Content-Length: %s, Content-Encoding: %s",
			c.Writer.Status(), contentLength, contentEncoding)
	})

	// Add CORS middleware
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Get handlers
	h := svc.GetHandlers()

	// API routes
	api := router.Group("/api/v3")
	{
		// WebSocket endpoint for report listening
		api.GET("/reports/listen", h.ListenReports)

		// Health check endpoint
		api.GET("/reports/health", h.HealthCheck)

		// Get last N analyzed reports endpoint
		api.GET("/reports/last", h.GetLastNAnalyzedReports)

		// Get report by sequence ID endpoint
		api.GET("/reports/by-seq", h.GetReportBySeq)

		// Get last N reports by ID endpoint
		api.GET("/reports/by-id", h.GetLastNReportsByID)

		// Get reports by latitude/longitude within radius endpoint
		api.GET("/reports/by-latlng", h.GetReportsByLatLng)

		// Get reports by brand name endpoint
		api.GET("/reports/by-brand", h.GetReportsByBrand)

		// Get image by sequence number endpoint
		api.GET("/reports/image", h.GetImageBySeq)
	}

	// Root health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"service": "report-listener",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	return router
}
