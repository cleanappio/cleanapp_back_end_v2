package main

import (
	"cleanapp-common/edge"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cleanapp-common/serverx"
	"report-ownership-service/config"
	"report-ownership-service/database"
	"report-ownership-service/service"
	"report-ownership-service/version"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to setup database:", err)
	}
	defer db.Close()
	ownershipService := database.NewOwnershipService(db)
	svc, err := service.NewService(cfg, ownershipService)
	if err != nil {
		log.Fatal("Failed to create service:", err)
	}
	if err := svc.Start(); err != nil {
		log.Fatal("Failed to start service:", err)
	}
	router := setupRouter(svc)
	srv := serverx.New(":8082", router)
	go func() {
		log.Printf("Starting HTTP server on port 8082")
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
	if err := svc.Stop(); err != nil {
		log.Printf("Error stopping service: %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
	log.Println("Server exited")
}

func setupDatabase(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	waitInterval := time.Second
	for {
		if err := db.Ping(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database ping timeout")
		}
		log.Printf("Database connection failed, retrying in %v", waitInterval)
		time.Sleep(waitInterval)
		if waitInterval < 4*time.Second {
			waitInterval *= 2
		}
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	log.Printf("Database connected successfully to %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)
	return db, nil
}

func setupRouter(svc *service.Service) *gin.Engine {
	router := gin.Default()
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, version.Get("report-ownership-service"))
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "service": "report-ownership-service", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	router.GET("/status", func(c *gin.Context) {
		status, err := svc.GetStatus()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get status: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": status.Status, "last_processed_seq": status.LastProcessedSeq, "total_reports": status.TotalReports, "last_update": status.LastUpdate.Format(time.RFC3339)})
	})
	return router
}
