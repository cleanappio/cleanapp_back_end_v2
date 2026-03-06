package main

import (
	"context"
	"email-service/config"
	"email-service/handlers"
	"email-service/service"
	"email-service/version"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil { log.Fatal("Failed to load email-service config:", err) }
	emailService, err := service.NewEmailService(cfg)
	if err != nil { log.Fatal("Failed to create email service:", err) }
	defer emailService.Close()
	if cfg.RunDBMigrations {
		log.Printf("Runtime DB migrations are disabled at service boot. Run email-service/cmd/migrate instead.")
	}
	handler := handlers.NewEmailServiceHandler(emailService)
	router := gin.Default()
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "OPTIONS"}}))
	router.LoadHTMLGlob("templates/*")
	apiV3 := router.Group("/api/v3")
	{
		apiV3.GET("/version", func(c *gin.Context) { c.JSON(200, version.Get("email-service")) })
		apiV3.POST("/optout", handler.HandleOptOut)
	}
	router.GET("/opt-out", handler.HandleOptOutLink)
	router.GET("/health", handler.HandleHealth)
	router.GET("/version", func(c *gin.Context) { c.JSON(200, version.Get("email-service")) })
	srv := serverx.New(":"+cfg.HTTPPort, router)
	go func() {
		log.Printf("HTTP server starting on port %s", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
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
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Server is shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}
