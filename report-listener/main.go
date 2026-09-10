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
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Printf("warning: failed to set trusted proxies: %v", err)
	}

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
		api.POST("/clusters/analyze", h.AnalyzeCluster)
		api.POST("/clusters/from-report", h.AnalyzeClusterFromReport)
		api.POST("/cases/match-cluster", h.MatchCluster)

		// Health check endpoint
		api.GET("/reports/health", h.HealthCheck)

		// Get last N analyzed reports endpoint
		api.GET("/reports/last", h.GetLastNAnalyzedReports)

		// Get last N reports by ID endpoint
		api.GET("/reports/by-id", h.GetLastNReportsByID)

		// Get reports by latitude/longitude within radius endpoint
		api.GET("/reports/by-latlng", h.GetReportsByLatLng)

		// Get reports by latitude/longitude within radius endpoint
		api.GET("/reports/by-latlng-lite", h.GetReportsByLatLngLite)

		// Sort game endpoints
		api.GET("/reports/sort/next", h.GetNextSortReport)
		api.POST("/reports/sort/submit", h.SubmitSortReport)

		// Get reports within a supplied polygonal geometry
		api.POST("/reports/by-geometry", h.GetReportsByGeometry)

		// Get case summaries linked to a report
		api.GET("/reports/cases", h.GetCasesByReportSeq)

		// Get reports by brand name endpoint
		api.GET("/reports/by-brand", h.GetReportsByBrand)

		// Search reports endpoint
		api.GET("/reports/search", h.GetSearchReports)
		api.POST("/reports/digital-share", h.SubmitDigitalShare)

		imageRoutes := api.Group("/reports")
		imageRoutes.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDetailRateLimitRPS,
				Burst: cfg.PublicDetailRateLimitBurst,
			}),
		)
		{
			imageRoutes.GET("/image", h.GetImageBySeq)
			imageRoutes.GET("/rawimage", h.GetRawImageBySeq)
			imageRoutes.GET("/image/by-public-id", h.GetImageByPublicID)
			imageRoutes.GET("/rawimage/by-public-id", h.GetRawImageByPublicID)
		}

		detailRoutes := api.Group("/reports")
		detailRoutes.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDetailRateLimitRPS,
				Burst: cfg.PublicDetailRateLimitBurst,
			}),
			middleware.PublicDetailAbuseMonitor(middleware.PublicDetailAbuseConfig{
				Window:    cfg.PublicDetailAbuseWindow,
				MaxHits:   cfg.PublicDetailAbuseMaxHits,
				MaxMisses: cfg.PublicDetailAbuseMaxMisses,
			}),
		)
		{
			detailRoutes.GET("/by-public-id", h.GetReportByPublicID)
			detailRoutes.GET("/by-seq", h.GetReportBySeq)
			detailRoutes.GET("/contact-strategy/by-public-id", h.GetReportContactStrategyByPublicID)
			detailRoutes.GET("/contact-strategy/by-seq", h.GetReportContactStrategyBySeq)
		}

		intelligenceRoutes := api.Group("/intelligence")
		intelligenceRoutes.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.IntelligenceRateLimitRPS,
				Burst: cfg.IntelligenceRateLimitBurst,
			}),
		)
		{
			intelligenceRoutes.POST("/query", h.QueryIntelligence)
		}

		publicWebSockets := api.Group("/public")
		publicWebSockets.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicWebSocketRateLimitRPS,
				Burst: cfg.PublicWebSocketRateLimitBurst,
			}),
		)
		{
			publicWebSockets.GET("/listen", h.ListenPublicReports)
		}

		privilegedWebSockets := api.Group("/reports")
		privilegedWebSockets.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PrivilegedWebSocketRateLimitRPS,
				Burst: cfg.PrivilegedWebSocketRateLimitBurst,
			}),
		)
		{
			// Backward-compatible path now downgrades anonymous callers to the public-lite stream.
			privilegedWebSockets.GET("/listen", h.ListenReports)
			privilegedWebSockets.GET("/listen/full", h.ListenPrivilegedReports)
		}

		publicDiscovery := api.Group("/public/discovery")
		publicDiscovery.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDiscoveryRateLimitRPS,
				Burst: cfg.PublicDiscoveryRateLimitBurst,
			}),
		)
		{
			publicDiscovery.GET("/last", h.GetPublicDiscoveryLast)
			publicDiscovery.GET("/search", h.GetPublicDiscoverySearch)
			publicDiscovery.GET("/by-brand", h.GetPublicDiscoveryByBrand)
			publicDiscovery.GET("/by-latlng", h.GetPublicDiscoveryByLatLng)
			publicDiscovery.GET("/brands/summary", h.GetPublicDiscoveryBrandSummaries)
			publicDiscovery.GET("/physical-points", h.GetPublicDiscoveryPhysicalPoints)
		}

		publicResolve := api.Group("/public")
		publicResolve.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicResolveRateLimitRPS,
				Burst: cfg.PublicResolveRateLimitBurst,
			}),
		)
		{
			publicResolve.GET("/resolve", h.ResolvePublicDiscoveryToken)
			publicResolve.GET("/resolve-physical-point", h.ResolvePhysicalMapPoint)
		}

		humanIngest := api.Group("/human-reports")
		humanIngest.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.HumanIngestRateLimitRPS,
				Burst: cfg.HumanIngestRateLimitBurst,
			}),
		)
		{
			humanIngest.POST("/submit", h.SubmitHumanReportV1)
			humanIngest.GET("/receipts/:receipt_id", h.GetHumanReceiptV1)
		}

		mobilePush := api.Group("/mobile/push")
		mobilePush.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.MobilePushRegisterRateLimitRPS,
				Burst: cfg.MobilePushRegisterRateLimitBurst,
			}),
		)
		{
			mobilePush.POST("/register", h.RegisterMobilePushDevice)
			mobilePush.POST("/unregister", h.UnregisterMobilePushDevice)
		}

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
			cases.POST("/upsert-from-cluster", h.UpsertCaseFromCluster)
			cases.POST("/merge", h.MergeCases)
			cases.GET("/:case_id", h.GetCase)
			cases.POST("/:case_id/reports", h.AddReportsToCase)
			cases.POST("/:case_id/status", h.UpdateCaseStatus)
			cases.GET("/:case_id/escalations", h.GetCaseEscalations)
			cases.POST("/:case_id/escalations/draft", h.DraftCaseEscalation)
			cases.POST("/:case_id/escalations/send", h.SendCaseEscalation)
			cases.POST("/:case_id/execution-tasks/:task_id/outcome", h.RecordCaseExecutionTaskOutcome)
		}

		reportActions := api.Group("/reports")
		reportActions.Use(middleware.AuthMiddleware(cfg, svc.GetHandlers().Db()))
		{
			reportActions.POST("/contact-strategy/by-public-id/:public_id/tasks/:task_id/outcome", h.RecordReportExecutionTaskOutcomeByPublicID)
			reportActions.POST("/contact-strategy/by-seq/:seq/tasks/:task_id/outcome", h.RecordReportExecutionTaskOutcomeBySeq)
		}
	}

	// API v4 routes (alias for v3 - for backwards compatibility with frontend)
	apiV4 := router.Group("/api/v4")
	{
		apiV4.GET("/version", versionHandler)
		apiV4.POST("/clusters/analyze", h.AnalyzeCluster)
		apiV4.POST("/clusters/from-report", h.AnalyzeClusterFromReport)
		apiV4.POST("/cases/match-cluster", h.MatchCluster)

		// Health check endpoint
		apiV4.GET("/reports/health", h.HealthCheck)

		// Get last N analyzed reports endpoint
		apiV4.GET("/reports/last", h.GetLastNAnalyzedReports)

		// Get last N reports by ID endpoint
		apiV4.GET("/reports/by-id", h.GetLastNReportsByID)

		// Get reports by latitude/longitude within radius endpoint
		apiV4.GET("/reports/by-latlng", h.GetReportsByLatLng)

		// Get reports by latitude/longitude within radius (lite) endpoint
		apiV4.GET("/reports/by-latlng-lite", h.GetReportsByLatLngLite)

		// Get reports within a supplied polygonal geometry
		apiV4.POST("/reports/by-geometry", h.GetReportsByGeometry)

		// Get case summaries linked to a report
		apiV4.GET("/reports/cases", h.GetCasesByReportSeq)

		// Get reports by brand name endpoint
		apiV4.GET("/reports/by-brand", h.GetReportsByBrand)

		// Search reports endpoint
		apiV4.GET("/reports/search", h.GetSearchReports)
		apiV4.POST("/reports/digital-share", h.SubmitDigitalShare)

		imageRoutesV4 := apiV4.Group("/reports")
		imageRoutesV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDetailRateLimitRPS,
				Burst: cfg.PublicDetailRateLimitBurst,
			}),
		)
		{
			imageRoutesV4.GET("/image", h.GetImageBySeq)
			imageRoutesV4.GET("/rawimage", h.GetRawImageBySeq)
			imageRoutesV4.GET("/image/by-public-id", h.GetImageByPublicID)
			imageRoutesV4.GET("/rawimage/by-public-id", h.GetRawImageByPublicID)
		}

		detailRoutesV4 := apiV4.Group("/reports")
		detailRoutesV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDetailRateLimitRPS,
				Burst: cfg.PublicDetailRateLimitBurst,
			}),
			middleware.PublicDetailAbuseMonitor(middleware.PublicDetailAbuseConfig{
				Window:    cfg.PublicDetailAbuseWindow,
				MaxHits:   cfg.PublicDetailAbuseMaxHits,
				MaxMisses: cfg.PublicDetailAbuseMaxMisses,
			}),
		)
		{
			detailRoutesV4.GET("/by-public-id", h.GetReportByPublicID)
			detailRoutesV4.GET("/by-seq", h.GetReportBySeq)
		}

		intelligenceRoutesV4 := apiV4.Group("/intelligence")
		intelligenceRoutesV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.IntelligenceRateLimitRPS,
				Burst: cfg.IntelligenceRateLimitBurst,
			}),
		)
		{
			intelligenceRoutesV4.POST("/query", h.QueryIntelligence)
		}

		publicWebSocketsV4 := apiV4.Group("/public")
		publicWebSocketsV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicWebSocketRateLimitRPS,
				Burst: cfg.PublicWebSocketRateLimitBurst,
			}),
		)
		{
			publicWebSocketsV4.GET("/listen", h.ListenPublicReports)
		}

		privilegedWebSocketsV4 := apiV4.Group("/reports")
		privilegedWebSocketsV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PrivilegedWebSocketRateLimitRPS,
				Burst: cfg.PrivilegedWebSocketRateLimitBurst,
			}),
		)
		{
			privilegedWebSocketsV4.GET("/listen", h.ListenReports)
			privilegedWebSocketsV4.GET("/listen/full", h.ListenPrivilegedReports)
		}

		publicDiscoveryV4 := apiV4.Group("/public/discovery")
		publicDiscoveryV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicDiscoveryRateLimitRPS,
				Burst: cfg.PublicDiscoveryRateLimitBurst,
			}),
		)
		{
			publicDiscoveryV4.GET("/last", h.GetPublicDiscoveryLast)
			publicDiscoveryV4.GET("/search", h.GetPublicDiscoverySearch)
			publicDiscoveryV4.GET("/by-brand", h.GetPublicDiscoveryByBrand)
			publicDiscoveryV4.GET("/by-latlng", h.GetPublicDiscoveryByLatLng)
			publicDiscoveryV4.GET("/brands/summary", h.GetPublicDiscoveryBrandSummaries)
			publicDiscoveryV4.GET("/physical-points", h.GetPublicDiscoveryPhysicalPoints)
		}

		publicResolveV4 := apiV4.Group("/public")
		publicResolveV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.PublicResolveRateLimitRPS,
				Burst: cfg.PublicResolveRateLimitBurst,
			}),
		)
		{
			publicResolveV4.GET("/resolve", h.ResolvePublicDiscoveryToken)
			publicResolveV4.GET("/resolve-physical-point", h.ResolvePhysicalMapPoint)
		}

		humanIngestV4 := apiV4.Group("/human-reports")
		humanIngestV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.HumanIngestRateLimitRPS,
				Burst: cfg.HumanIngestRateLimitBurst,
			}),
		)
		{
			humanIngestV4.POST("/submit", h.SubmitHumanReportV1)
			humanIngestV4.GET("/receipts/:receipt_id", h.GetHumanReceiptV1)
		}

		mobilePushV4 := apiV4.Group("/mobile/push")
		mobilePushV4.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.MobilePushRegisterRateLimitRPS,
				Burst: cfg.MobilePushRegisterRateLimitBurst,
			}),
		)
		{
			mobilePushV4.POST("/register", h.RegisterMobilePushDevice)
			mobilePushV4.POST("/unregister", h.UnregisterMobilePushDevice)
		}

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
			casesV4.POST("/upsert-from-cluster", h.UpsertCaseFromCluster)
			casesV4.POST("/merge", h.MergeCases)
			casesV4.GET("/:case_id", h.GetCase)
			casesV4.POST("/:case_id/reports", h.AddReportsToCase)
			casesV4.POST("/:case_id/status", h.UpdateCaseStatus)
			casesV4.GET("/:case_id/escalations", h.GetCaseEscalations)
			casesV4.POST("/:case_id/escalations/draft", h.DraftCaseEscalation)
			casesV4.POST("/:case_id/escalations/send", h.SendCaseEscalation)
			casesV4.POST("/:case_id/execution-tasks/:task_id/outcome", h.RecordCaseExecutionTaskOutcome)
		}

		reportActionsV4 := apiV4.Group("/reports")
		reportActionsV4.Use(middleware.AuthMiddleware(cfg, svc.GetHandlers().Db()))
		{
			reportActionsV4.POST("/contact-strategy/by-public-id/:public_id/tasks/:task_id/outcome", h.RecordReportExecutionTaskOutcomeByPublicID)
			reportActionsV4.POST("/contact-strategy/by-seq/:seq/tasks/:task_id/outcome", h.RecordReportExecutionTaskOutcomeBySeq)
		}
	}

	// Canonical intelligence endpoint
	intelligenceAlias := router.Group("/api/intelligence")
	intelligenceAlias.Use(
		edge.RateLimitMiddleware(edge.RateLimitConfig{
			RPS:   cfg.IntelligenceRateLimitRPS,
			Burst: cfg.IntelligenceRateLimitBurst,
		}),
	)
	{
		intelligenceAlias.POST("/query", h.QueryIntelligence)
	}

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
		humanIngestV1 := apiV1.Group("/human-reports")
		humanIngestV1.Use(
			edge.RateLimitMiddleware(edge.RateLimitConfig{
				RPS:   cfg.HumanIngestRateLimitRPS,
				Burst: cfg.HumanIngestRateLimitBurst,
			}),
		)
		{
			humanIngestV1.POST("/submit", h.SubmitHumanReportV1)
			humanIngestV1.GET("/receipts/:receipt_id", h.GetHumanReceiptV1)
		}

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
		internal.POST("/mobile-push/report-deliveries", h.PushReportDeliveryUpdate)
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
	router.POST("/api/reports/digital-share", h.SubmitDigitalShare)

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
