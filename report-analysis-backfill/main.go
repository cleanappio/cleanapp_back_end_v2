package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"report-analysis-backfill/config"
	"report-analysis-backfill/database"
	"report-analysis-backfill/handlers"
	"report-analysis-backfill/service"
	"report-analysis-backfill/version"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting report analysis backfill service with poll_interval=%v batch_size=%d report_analysis_url=%s", cfg.PollInterval, cfg.BatchSize, cfg.ReportAnalysisURL)

	db, err := database.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	backfillService := service.NewBackfillService(cfg, db)
	h := handlers.NewHandlers(backfillService)

	router := gin.Default()
	if len(cfg.TrustedProxies) > 0 {
		if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			log.Printf("Failed to set trusted proxies: %v", err)
		}
	}
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "OPTIONS"}, AllowCredentials: true}))

	router.GET("/health", h.HealthCheck)
	router.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, version.Get("report-analysis-backfill"))
	})

	api := router.Group("/api/v1")
	{
		api.GET("/health", h.HealthCheck)
		api.GET("/status", h.GetStatus)
	}

	srv := serverx.New(":"+cfg.Port, router)
	backfillService.Start()

	go func() {
		log.Printf("Starting HTTP server on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	backfillService.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}
