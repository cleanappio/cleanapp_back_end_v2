package main

import (
	"context"
	"email-service/config"
	"email-service/handlers"
	"email-service/service"
	"email-service/version"
	"encoding/json"
	"flag"
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
	notificationPlan := flag.Bool("notification-plan", false, "print a read-only verified-company notification plan and exit")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load email-service config:", err)
	}
	if *notificationPlan {
		planService, err := service.NewNotificationPlanService(context.Background(), cfg)
		if err != nil {
			log.Fatal("Failed to create notification plan reader: ", err)
		}
		defer planService.Close()
		if err := json.NewEncoder(os.Stdout).Encode(planService.BuildCompanyNotificationPlans(context.Background(), time.Now().UTC())); err != nil {
			log.Fatal(err)
		}
		return
	}
	emailService, err := service.NewEmailService(cfg)
	if err != nil {
		log.Fatal("Failed to create email service:", err)
	}
	defer emailService.Close()
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
	internal := router.Group("/internal")
	internal.Use(handlers.InternalAdminToken(cfg.InternalAdminToken))
	{
		internal.POST("/case-escalations/send", handler.HandleSendCaseEscalation)
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
	// Run this bounded priority route separately: the legacy global aggregation
	// can take several minutes and must not delay verified fresh reports.
	go func() {
		const interval = time.Minute
		for {
			if err := emailService.ProcessCompanyNotifications(); err != nil {
				log.Printf("Error processing verified company notifications: %v", err)
			}
			time.Sleep(interval)
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
