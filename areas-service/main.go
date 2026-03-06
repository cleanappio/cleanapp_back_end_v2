package main

import (
	"areas-service/config"
	"areas-service/database"
	"areas-service/handlers"
	"areas-service/middleware"
	"areas-service/utils"
	"areas-service/version"
	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	EndPointHealth             = "/health"
	EndPointCreateOrUpdateArea = "/create_or_update_area"
	EndPointGetAreas           = "/get_areas"
	EndPointUpdateConsent      = "/update_consent"
	EndPointGetAreasCount      = "/get_areas_count"
	EndPointDeleteArea         = "/delete_area"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	log.Println("Starting the areas service...")

	db, err := utils.DBConnect(cfg)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	areasService := database.NewAreasService(db)
	areasHandler := handlers.NewAreasHandler(areasService)

	router := gin.Default()
	if len(cfg.TrustedProxies) > 0 {
		if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			log.Printf("Failed to set trusted proxies: %v", err)
		}
	}
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst}))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "OPTIONS", "DELETE"}}))

	router.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, version.Get("areas-service"))
	})
	router.GET(EndPointHealth, areasHandler.HealthCheck)

	apiV3 := router.Group("/api/v3")
	{
		apiV3.POST(EndPointCreateOrUpdateArea, middleware.AuthMiddleware(cfg, db), areasHandler.CreateOrUpdateArea)
		apiV3.GET(EndPointGetAreas, areasHandler.GetAreas)
		apiV3.POST(EndPointUpdateConsent, areasHandler.UpdateConsent)
		apiV3.GET(EndPointGetAreasCount, areasHandler.GetAreasCount)
		apiV3.DELETE(EndPointDeleteArea, middleware.AuthMiddleware(cfg, db), areasHandler.DeleteArea)
	}

	srv := serverx.New(fmt.Sprintf(":%s", cfg.Port), router)
	go func() {
		log.Printf("Areas service starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
}
