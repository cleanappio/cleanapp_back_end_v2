package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-ownership-service/config"
	"report-ownership-service/database"
	"report-ownership-service/service"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Setup database connection
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to setup database:", err)
	}
	defer db.Close()

	// Initialize database schema
	if err := database.InitializeSchema(db); err != nil {
		log.Fatal("Failed to initialize database schema:", err)
	}

	// Create ownership service
	ownershipService := database.NewOwnershipService(db)
	svc, err := service.NewService(cfg, ownershipService)
	if err != nil {
		log.Fatal("Failed to create service:", err)
	}

	// Start service
	if err := svc.Start(); err != nil {
		log.Fatal("Failed to start service:", err)
	}

	// Setup HTTP server for status monitoring
	router := setupRouter(svc)

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":8082", // Different port from other services
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on port 8082")
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

func setupDatabase(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection with exponential backoff retry
	var waitInterval time.Duration = 1 * time.Second
	for {
		if err := db.Ping(); err == nil {
			break // Connection successful
		}
		log.Printf("Database connection failed, retrying in %v: %v", waitInterval, err)
		time.Sleep(waitInterval)
		waitInterval *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, ...
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Printf("Database connected successfully to %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	return db, nil
}

func setupRouter(svc *service.Service) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "report-ownership-service",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Status endpoint
	router.GET("/status", func(c *gin.Context) {
		status, err := svc.GetStatus()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to get status: %v", err),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":             status.Status,
			"last_processed_seq": status.LastProcessedSeq,
			"total_reports":      status.TotalReports,
			"last_update":        status.LastUpdate.Format(time.RFC3339),
		})
	})

	return router
}
