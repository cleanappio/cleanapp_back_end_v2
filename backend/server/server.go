package server

import (
	"cleanapp-common/appenv"
	"cleanapp-common/edge"
	"cleanapp-common/serverx"
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os/signal"
	"syscall"
	"time"

	"cleanapp/backend/rabbitmq"
	"cleanapp/common/version"
	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	EndPointHelp              = "/help"
	EndPointUser              = "/update_or_create_user"
	EndPointReport            = "/report"
	EndPointReadReport        = "/read_report"
	EndPointGetMap            = "/get_map"
	EndPointGetStats          = "/get_stats"
	EndPointGetTeams          = "/get_teams"
	EndPointGetTopScores      = "/get_top_scores"
	EndPointPrivacyAndTOC     = "/update_privacy_and_toc"
	EndPointReadReferral      = "/read_referral"
	EndPointWriteReferral     = "/write_referral"
	EndPointGenerateReferral  = "/generate_referral"
	EndPointGetBlockChainLink = "/get_blockchain_link"
	EndPointGetActions        = "/get_actions"
	EndPointGetAction         = "/get_action"
	EndPointCreateAction      = "/create_action"
	EndPointUpdateAction      = "/update_action"
	EndPointDeleteAction      = "/delete_action"
	EndPointUpdateUserAction  = "/update_user_action"
	EndPointGetAreas          = "/get_areas"
	EndPointValidReportsCount = "/valid-reports-count"
	EndPointVersion           = "/version"
)

var serverPort = flag.Int("port", 8080, "The port used by the service.")
var rabbitmqPublisher *rabbitmq.Publisher
var userRoutingKey string

func getRabbitMQConfig() (string, string, string, string, error) {
	host := appenv.String("AMQP_HOST", "localhost")
	port := appenv.String("AMQP_PORT", "5672")
	user, err := appenv.StringRequiredInProd("AMQP_USER", "cleanapp")
	if err != nil {
		return "", "", "", "", err
	}
	password, err := appenv.Secret("AMQP_PASSWORD", "")
	if err != nil {
		return "", "", "", "", err
	}
	amqpURL := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, password, host, port)
	exchangeName := appenv.String("RABBITMQ_EXCHANGE", "cleanapp")
	reportRoutingKey := appenv.String("RABBITMQ_RAW_REPORT_ROUTING_KEY", "report.raw")
	userRoutingKey := appenv.String("RABBITMQ_USER_ROUTING_KEY", "user.add")
	return amqpURL, exchangeName, reportRoutingKey, userRoutingKey, nil
}

func initializePublisher() error {
	amqpURL, exchangeName, reportRoutingKey, userRoutingKeyFromEnv, err := getRabbitMQConfig()
	if err != nil {
		return fmt.Errorf("failed to load RabbitMQ config: %w", err)
	}
	publisher, err := rabbitmq.NewPublisher(amqpURL, exchangeName, reportRoutingKey)
	if err != nil {
		return fmt.Errorf("failed to initialize RabbitMQ publisher: %w", err)
	}
	rabbitmqPublisher = publisher
	userRoutingKey = userRoutingKeyFromEnv
	log.Infof("RabbitMQ publisher initialized: exchange=%s, report_routing_key=%s, user_routing_key=%s", exchangeName, reportRoutingKey, userRoutingKeyFromEnv)
	return nil
}

func closePublisher() {
	if rabbitmqPublisher != nil {
		if err := rabbitmqPublisher.Close(); err != nil {
			log.Errorf("Failed to close RabbitMQ publisher: %v", err)
		} else {
			log.Info("RabbitMQ publisher closed successfully")
		}
	}
}

func StartService() {
	log.Info("Starting the service...")
	defer closeServerDB()
	if err := initializePublisher(); err != nil {
		log.Errorf("Failed to initialize RabbitMQ publisher: %v", err)
		log.Info("Continuing without RabbitMQ publisher...")
	}
	StartCounterCacheUpdater()
	StartBrandReportCountsUpdater()
	defer closePublisher()
	router := gin.Default()
	router.Use(edge.SecurityHeaders())
	router.Use(edge.RequestBodyLimit(1 << 20))
	router.Use(edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: float64(appenv.Int("RATE_LIMIT_RPS", 20)), Burst: appenv.Int("RATE_LIMIT_BURST", 40)}))
	router.Use(edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: allowedOrigins(), AllowedMethods: []string{"GET", "POST", "OPTIONS"}, AllowCredentials: true}))
	router.GET(EndPointVersion, func(c *gin.Context) { c.JSON(http.StatusOK, version.Get("cleanapp-service")) })
	router.GET(EndPointHelp, Help)
	router.GET(EndPointGetAreas, GetAreas)
	router.GET(EndPointValidReportsCount, GetValidReportsCount)
	router.POST(EndPointUser, CreateOrUpdateUser)
	router.POST(EndPointPrivacyAndTOC, UpdatePrivacyAndTOC)
	router.POST(EndPointReport, Report)
	router.POST(EndPointReadReport, ReadReport)
	router.POST(EndPointGetMap, GetMap)
	router.POST(EndPointGetStats, GetStats)
	router.POST(EndPointGetTeams, GetTeams)
	router.POST(EndPointGetTopScores, GetTopScores)
	router.POST(EndPointReadReferral, ReadReferral)
	router.POST(EndPointWriteReferral, WriteReferral)
	router.POST(EndPointGenerateReferral, GenerateReferral)
	router.POST(EndPointGetBlockChainLink, GetBlockchainLink)
	router.POST(EndPointCreateAction, CreateAction)
	router.POST(EndPointUpdateAction, UpdateAction)
	router.POST(EndPointDeleteAction, DeleteAction)
	router.GET(EndPointGetActions, GetActions)
	router.GET(EndPointGetAction, GetAction)
	router.POST(EndPointUpdateUserAction, UpdateUserAction)
	srv := serverx.New(fmt.Sprintf(":%d", *serverPort), router)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Service failed: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Failed to shutdown service cleanly: %v", err)
	}
}

func allowedOrigins() []string {
	if origins := appenv.Strings("ALLOWED_ORIGINS"); len(origins) > 0 {
		return origins
	}
	frontendURL := appenv.String("FRONTEND_URL", "https://cleanapp.io")
	origins := []string{frontendURL}
	if frontendURL == "https://cleanapp.io" {
		origins = append(origins, "https://www.cleanapp.io")
	}
	return origins
}

func GetAreas(c *gin.Context) {
	targetURL := "https://areas.cleanapp.io/api/v3/get_areas"
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		log.Errorf("Failed to parse target URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid target URL"})
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, parsedURL.String())
}
