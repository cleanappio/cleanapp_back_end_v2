package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"brand-dashboard/config"
	"brand-dashboard/handlers"
	"brand-dashboard/middleware"
	"brand-dashboard/services"
	"brand-dashboard/version"
	"cleanapp-common/edge"
	"cleanapp-common/serverx"

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
	log.Printf("INFO: Brand dashboard configured with brands: %v", cfg.BrandNames)
	databaseService, err := services.NewDatabaseService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database service: %v", err)
	}
	defer databaseService.Close()
	websocketHub := services.NewWebSocketHub()
	go websocketHub.Start()
	defer websocketHub.Stop()
	brandHandler := handlers.NewBrandHandler(databaseService)
	websocketHandler := handlers.NewWebSocketHandler(websocketHub, cfg.AllowedOrigins)

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

	r.GET("/health", brandHandler.HealthHandler)
	r.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("brand-dashboard"))
	})

	protected := r.Group("/")
	protected.Use(middleware.AuthMiddleware(cfg, databaseService.DB()))
	{
		protected.GET("/brands", brandHandler.BrandsHandler)
		protected.GET("/reports", brandHandler.ReportsHandler)
		protected.GET("/ws/brand-reports", websocketHandler.ListenBrandReports)
		protected.GET("/ws/health", websocketHandler.HealthCheck)
	}

	srv := serverx.New(cfg.Host+":"+cfg.Port, r)
	go func() {
		log.Printf("Starting Brand Dashboard service on %s:%s", cfg.Host, cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
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
