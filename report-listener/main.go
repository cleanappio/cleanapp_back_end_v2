package main

import (
	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"report-listener/config"
	"report-listener/middleware"
	"report-listener/service"
	"report-listener/version"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

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
	router := setupRouter(cfg, svc)
	handler := cleanAppWireAliasHandler(router)

	// Create HTTP server
	srv := serverx.New(":"+cfg.Port, handler)

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

func setupRouter(cfg *config.Config, svc *service.Service) *gin.Engine {
	router := gin.Default()

	// Add gzip compression middleware
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(edge.RequestBodyLimit(cfg.RequestBodyLimitBytes))
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{
		RPS:   cfg.RateLimitRPS,
		Burst: cfg.RateLimitBurst,
	}))

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

	router.Use(edge.CORSMiddleware(edge.CORSConfig{
		AllowedOrigins: cfg.AllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
	}))

	// Get handlers
	h := svc.GetHandlers()

	versionHandler := func(c *gin.Context) {
		c.JSON(200, version.Get("report-listener"))
	}

	// API routes
	api := router.Group("/api/v3")
	{
		api.GET("/version", versionHandler)
		api.POST("/intelligence/query", h.QueryIntelligence)
		api.POST("/clusters/analyze", h.AnalyzeCluster)
		api.POST("/clusters/from-report", h.AnalyzeClusterFromReport)

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

		// Get reports by latitude/longitude within radius endpoint
		api.GET("/reports/by-latlng-lite", h.GetReportsByLatLngLite)

		// Get reports within a supplied polygonal geometry
		api.POST("/reports/by-geometry", h.GetReportsByGeometry)

		// Get reports by brand name endpoint
		api.GET("/reports/by-brand", h.GetReportsByBrand)

		// Search reports endpoint
		api.GET("/reports/search", h.GetSearchReports)

		// Get image by sequence number endpoint
		api.GET("/reports/image", h.GetImageBySeq)

		// Get raw image by sequence number endpoint
		api.GET("/reports/rawimage", h.GetRawImageBySeq)

		// Protected bulk ingest endpoint
		protected := api.Group("/reports")
		protected.Use(middleware.FetcherAuthMiddleware(svc.GetHandlers().Db()))
		{
			protected.POST("/bulk_ingest", h.BulkIngest)
		}

		cases := api.Group("/cases")
		cases.Use(middleware.AuthMiddleware(cfg, svc.GetHandlers().Db()))
		{
			cases.POST("", h.CreateCase)
			cases.GET("/:case_id", h.GetCase)
			cases.POST("/:case_id/reports", h.AddReportsToCase)
			cases.POST("/:case_id/status", h.UpdateCaseStatus)
		}
	}

	// API v4 routes (alias for v3 - for backwards compatibility with frontend)
	apiV4 := router.Group("/api/v4")
	{
		apiV4.GET("/version", versionHandler)
		apiV4.POST("/intelligence/query", h.QueryIntelligence)
		apiV4.POST("/clusters/analyze", h.AnalyzeCluster)
		apiV4.POST("/clusters/from-report", h.AnalyzeClusterFromReport)

		// WebSocket endpoint for report listening
		apiV4.GET("/reports/listen", h.ListenReports)

		// Health check endpoint
		apiV4.GET("/reports/health", h.HealthCheck)

		// Get last N analyzed reports endpoint
		apiV4.GET("/reports/last", h.GetLastNAnalyzedReports)

		// Get report by sequence ID endpoint
		apiV4.GET("/reports/by-seq", h.GetReportBySeq)

		// Get last N reports by ID endpoint
		apiV4.GET("/reports/by-id", h.GetLastNReportsByID)

		// Get reports by latitude/longitude within radius endpoint
		apiV4.GET("/reports/by-latlng", h.GetReportsByLatLng)

		// Get reports by latitude/longitude within radius (lite) endpoint
		apiV4.GET("/reports/by-latlng-lite", h.GetReportsByLatLngLite)

		// Get reports within a supplied polygonal geometry
		apiV4.POST("/reports/by-geometry", h.GetReportsByGeometry)

		// Get reports by brand name endpoint
		apiV4.GET("/reports/by-brand", h.GetReportsByBrand)

		// Search reports endpoint
		apiV4.GET("/reports/search", h.GetSearchReports)

		// Get image by sequence number endpoint
		apiV4.GET("/reports/image", h.GetImageBySeq)

		// Get raw image by sequence number endpoint
		apiV4.GET("/reports/rawimage", h.GetRawImageBySeq)

		// Protected bulk ingest endpoint
		protectedV4 := apiV4.Group("/reports")
		protectedV4.Use(middleware.FetcherAuthMiddleware(svc.GetHandlers().Db()))
		{
			protectedV4.POST("/bulk_ingest", h.BulkIngest)
		}

		casesV4 := apiV4.Group("/cases")
		casesV4.Use(middleware.AuthMiddleware(cfg, svc.GetHandlers().Db()))
		{
			casesV4.POST("", h.CreateCase)
			casesV4.GET("/:case_id", h.GetCase)
			casesV4.POST("/:case_id/reports", h.AddReportsToCase)
			casesV4.POST("/:case_id/status", h.UpdateCaseStatus)
		}
	}

	// Canonical intelligence endpoint
	router.POST("/api/intelligence/query", h.QueryIntelligence)

	// Public ingest v1 (Fetcher Key System + quarantine lane)
	v1 := router.Group("/v1")
	{
		v1.GET("/openapi.yaml", h.ServeIngestOpenAPI)
		v1.GET("/docs", h.ServeIngestSwaggerUI)

		v1.POST("/fetchers/register", h.RegisterFetcherV1)
		v1Auth := v1.Group("/")
		v1Auth.Use(middleware.FetcherKeyAuthV1(h.Db(), cfg.FetcherKeyEnv, "fetcher:read"))
		{
			v1Auth.GET("/fetchers/me", h.GetFetcherMeV1)
			v1Auth.POST("/fetchers/promotion-request", h.CreateFetcherPromotionRequestV1)
			v1Auth.GET("/fetchers/promotion-status", h.GetFetcherPromotionStatusV1)
		}
		v1Ingest := v1.Group("/")
		v1Ingest.Use(middleware.FetcherKeyAuthV1(h.Db(), cfg.FetcherKeyEnv, "report:submit"))
		{
			v1Ingest.POST("/reports:bulkIngest", h.BulkIngestV1)
		}
	}

	// CleanApp Wire v1 (canonical agent submission surface).
	apiV1 := router.Group("/api/v1")
	{
		apiV1.GET("/openapi.yaml", h.ServeCleanAppWireOpenAPI)
		apiV1.GET("/docs", h.ServeCleanAppWireSwaggerUI)

		apiV1.POST("/agents/register", h.RegisterAgentV1)

		apiV1Auth := apiV1.Group("/")
		apiV1Auth.Use(middleware.FetcherKeyAuthV1(h.Db(), cfg.FetcherKeyEnv, "fetcher:read"))
		{
			apiV1Auth.GET("/agents/me", h.GetAgentMeV1)
			apiV1Auth.GET("/agents/reputation/:agent_id", h.GetAgentReputationV1)
			apiV1Auth.GET("/agent-reports/receipts/:receipt_id", h.GetCleanAppWireReceiptV1)
			apiV1Auth.GET("/agent-reports/status/:source_id", h.GetCleanAppWireStatusV1)
		}

		apiV1Submit := apiV1.Group("/")
		apiV1Submit.Use(middleware.FetcherKeyAuthV1(h.Db(), cfg.FetcherKeyEnv, "report:submit"))
		{
			apiV1Submit.POST("/agent-reports/submit", h.SubmitCleanAppWireV1)
			apiV1Submit.POST("/agent-reports/batch-submit", h.BatchSubmitCleanAppWireV1)
		}
	}

	// Internal admin (kill switches / promotion hooks).
	internal := router.Group("/internal")
	internal.Use(middleware.InternalAdminToken(cfg.InternalAdminToken))
	{
		internal.POST("/reports/:seq/promote", h.InternalPromoteReport)
		internal.POST("/fetchers/:fetcher_id/suspend", h.InternalSuspendFetcher)
		internal.POST("/fetchers/keys/:key_id/revoke", h.InternalRevokeFetcherKey)
		internal.GET("/fetchers/promotion-requests", h.InternalListPromotionRequests)
		internal.POST("/fetchers/promotion-requests/:id/decide", h.InternalDecidePromotionRequest)
	}

	// Root health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"service": "report-listener",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})
	router.GET("/version", versionHandler)

	return router
}

func cleanAppWireAliasHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/v1/agent-reports:submit":
				r.URL.Path = "/api/v1/agent-reports/submit"
			case "/api/v1/agent-reports:batchSubmit":
				r.URL.Path = "/api/v1/agent-reports/batch-submit"
			}
		}
		next.ServeHTTP(w, r)
	})
}
