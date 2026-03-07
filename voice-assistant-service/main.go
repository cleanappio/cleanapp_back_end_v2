package main

import (
	"context"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"voice-assistant-service/config"
	"voice-assistant-service/handlers"
	"voice-assistant-service/version"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHealth     = "/health"
	EndPointSession    = "/session"
	EndPointPrewarm    = "/session/prewarm"
	EndPointProxyOffer = "/webrtc/proxy-offer"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Info("Starting the voice assistant service...")

	// Initialize handlers
	sessionHandler := handlers.NewSessionHandler(cfg)
	webrtcHandler := handlers.NewWebRTCHandler()

	// Setup router
	router := gin.Default()
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "OPTIONS"}}))

	// Health check endpoint (no auth required)
	router.GET(EndPointHealth, func(c *gin.Context) {
		info := version.Get("voice-assistant-service")
		c.JSON(200, gin.H{
			"status":     "healthy",
			"service":    info.Service,
			"version":    info.Version,
			"git_sha":    info.GitSHA,
			"build_time": info.BuildTime,
		})
	})

	router.GET("/version", func(c *gin.Context) {
		c.JSON(200, version.Get("voice-assistant-service"))
	})

	// Rate-limited endpoints (no auth required for mobile app compatibility)
	rateLimited := router.Group("/")
	rateLimited.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	{
		// Session management
		rateLimited.POST(EndPointSession, sessionHandler.CreateEphemeralSession)
		rateLimited.POST(EndPointPrewarm, sessionHandler.PrewarmSession)

		// WebRTC proxy (optional)
		rateLimited.POST(EndPointProxyOffer, webrtcHandler.ProxyOffer)
	}

	srv := serverx.New(":"+cfg.Port, router)
	log.Infof("Voice assistant service starting on port %s", cfg.Port)
	log.Infof("Rate limit: rps=%.3f burst=%d", cfg.RateLimitRPS, cfg.RateLimitBurst)
	log.Infof("Allowed origins: %v", cfg.AllowedOrigins)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Failed to shutdown voice assistant service cleanly: %v", err)
	}
}
