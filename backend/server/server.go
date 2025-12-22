package server

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"cleanapp/backend/rabbitmq"

	"github.com/apex/log"
	"github.com/gin-contrib/cors"
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
)

var (
	serverPort = flag.Int("port", 8080, "The port used by the service.")
)

// Global RabbitMQ publisher instance
var rabbitmqPublisher *rabbitmq.Publisher

// Global user routing key
var userRoutingKey string

// getRabbitMQConfig returns RabbitMQ configuration from environment variables
func getRabbitMQConfig() (string, string, string, string) {
	// Get AMQP URL components
	host := os.Getenv("AMQP_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("AMQP_PORT")
	if port == "" {
		port = "5672"
	}

	user := os.Getenv("AMQP_USER")
	if user == "" {
		user = "guest"
	}

	password := os.Getenv("AMQP_PASSWORD")
	if password == "" {
		password = "guest"
	}

	// Construct AMQP URL
	amqpURL := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, password, host, port)

	// Get exchange name
	exchangeName := os.Getenv("RABBITMQ_EXCHANGE")
	if exchangeName == "" {
		exchangeName = "cleanapp"
	}

	// Get report routing key
	reportRoutingKey := os.Getenv("RABBITMQ_RAW_REPORT_ROUTING_KEY")
	if reportRoutingKey == "" {
		reportRoutingKey = "report.raw"
	}

	// Get user routing key
	userRoutingKey := os.Getenv("RABBITMQ_USER_ROUTING_KEY")
	if userRoutingKey == "" {
		userRoutingKey = "user.add"
	}

	return amqpURL, exchangeName, reportRoutingKey, userRoutingKey
}

// initializePublisher initializes the RabbitMQ publisher for reports and user events
func initializePublisher() error {
	amqpURL, exchangeName, reportRoutingKey, userRoutingKeyFromEnv := getRabbitMQConfig()

	publisher, err := rabbitmq.NewPublisher(amqpURL, exchangeName, reportRoutingKey)
	if err != nil {
		return fmt.Errorf("failed to initialize RabbitMQ publisher: %w", err)
	}

	rabbitmqPublisher = publisher
	// Store the user routing key globally for use in user events
	userRoutingKey = userRoutingKeyFromEnv
	log.Infof("RabbitMQ publisher initialized: exchange=%s, report_routing_key=%s, user_routing_key=%s",
		exchangeName, reportRoutingKey, userRoutingKeyFromEnv)
	return nil
}

// closePublisher closes the RabbitMQ publisher
func closePublisher() {
	if rabbitmqPublisher != nil {
		err := rabbitmqPublisher.Close()
		if err != nil {
			log.Errorf("Failed to close RabbitMQ publisher: %v", err)
		} else {
			log.Info("RabbitMQ publisher closed successfully")
		}
	}
}

func StartService() {
	log.Info("Starting the service...")

	// Initialize RabbitMQ publisher for reports and user events
	err := initializePublisher()
	if err != nil {
		log.Errorf("Failed to initialize RabbitMQ publisher: %v", err)
		log.Info("Continuing without RabbitMQ publisher...")
	}

	// Start background counter cache updater
	// This uses incremental counting to avoid slow full table scans
	StartCounterCacheUpdater()

	// Ensure cleanup on exit
	defer closePublisher()

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET(EndPointHelp, Help)
	router.GET(EndPointGetAreas, GetAreas) // +
	router.GET(EndPointValidReportsCount, GetValidReportsCount)
	router.POST(EndPointUser, CreateOrUpdateUser)             // +
	router.POST(EndPointPrivacyAndTOC, UpdatePrivacyAndTOC)   // +
	router.POST(EndPointReport, Report)                       // +
	router.POST(EndPointReadReport, ReadReport)               // +
	router.POST(EndPointGetMap, GetMap)                       // +
	router.POST(EndPointGetStats, GetStats)                   // +
	router.POST(EndPointGetTeams, GetTeams)                   // +
	router.POST(EndPointGetTopScores, GetTopScores)           // +
	router.POST(EndPointReadReferral, ReadReferral)           // get -> post
	router.POST(EndPointWriteReferral, WriteReferral)         // Missing
	router.POST(EndPointGenerateReferral, GenerateReferral)   // +
	router.POST(EndPointGetBlockChainLink, GetBlockchainLink) // +
	router.POST(EndPointCreateAction, CreateAction)           // Missing
	router.POST(EndPointUpdateAction, UpdateAction)           // modifyAction
	router.POST(EndPointDeleteAction, DeleteAction)           // Missing
	router.GET(EndPointGetActions, GetActions)                // post -> get
	router.GET(EndPointGetAction, GetAction)                  // Missing
	router.POST(EndPointUpdateUserAction, UpdateUserAction)   // userAction?

	router.Run(fmt.Sprintf(":%d", *serverPort))
	log.Info("Finished the service. Should not ever being seen.")
}

func GetAreas(c *gin.Context) {
	// Build the target URL with the same query string
	targetURL := "https://areas.cleanapp.io/api/v3/get_areas"
	if c.Request.URL.RawQuery != "" {
		targetURL += "?" + c.Request.URL.RawQuery
	}

	// Parse the target URL to ensure it's valid
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		log.Errorf("Failed to parse target URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid target URL"})
		return
	}

	// Redirect to the areas service
	c.Redirect(http.StatusTemporaryRedirect, parsedURL.String())
}
