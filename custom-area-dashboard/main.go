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
	"custom-area-dashboard/config"
	"custom-area-dashboard/handlers"
	"custom-area-dashboard/middleware"
	"custom-area-dashboard/services"
	"custom-area-dashboard/version"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found, using system environment variables")
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	databaseService, err := services.NewDatabaseService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database service: %v", err)
	}
	defer databaseService.Close()

	areasHandler := handlers.NewAreasHandler(databaseService, cfg)
	r := gin.Default()
	if len(cfg.TrustedProxies) > 0 {
		if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			log.Printf("Failed to set trusted proxies: %v", err)
		}
	}
	r.Use(edge.SecurityHeaders())
	r.Use(edge.RequestBodyLimit(1 << 20))
	r.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	r.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}, AllowCredentials: true}))

	r.GET("/health", areasHandler.HealthHandler)
	r.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("custom-area-dashboard"))
	})

	group := r.Group("/")
	log.Printf("INFO: Public dashboard: %v", cfg.IsPublic)
	if !cfg.IsPublic {
		group.Use(middleware.AuthMiddleware(cfg))
	}
	{
		group.GET("/areas", areasHandler.AreasHandler)
		group.GET("/sub_areas", areasHandler.SubAreasHandler)
		group.GET("/reports", areasHandler.ReportsHandler)
		group.GET("/reports_aggr", areasHandler.ReportsAggrHandler)
	}

	srv := serverx.New(cfg.Host+":"+cfg.Port, r)
	go func() {
		log.Printf("Starting Custom Area Dashboard service on %s:%s", cfg.Host, cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Dashboard server failed: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
}
