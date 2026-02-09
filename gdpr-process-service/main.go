package main

import (
	"encoding/json"
	"fmt"
	"gdpr-process-service/config"
	"gdpr-process-service/database"
	"gdpr-process-service/face_detector"
	"gdpr-process-service/openai"
	"gdpr-process-service/processor"
	"gdpr-process-service/utils"
	"log"

	"gdpr-process-service/rabbitmq"
)

// Message types for RabbitMQ
type UserMessage struct {
	Version  string `json:"version"`
	Id       string `json:"id"`
	Avatar   string `json:"avatar"`
	Referral string `json:"referral"`
}

type ReportMessage struct {
	Seq         int     `json:"seq"`
	Timestamp   string  `json:"timestamp"`
	ID          string  `json:"id"`
	Team        int     `json:"team"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	ActionID    string  `json:"action_id"`
	Description string  `json:"description"`
}

func main() {
	// Load configuration
	cfg := config.Load()

	log.Printf("Starting the GDPR process service...")

	// Connect to database
	db, err := utils.DBConnect(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	if err := database.InitSchema(db); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)

	// Initialize face detector client
	faceDetectorClient := face_detector.NewClient(cfg.FaceDetectorURL, cfg.FaceDetectorPortStart)

	// Initialize services
	gdprService := database.NewGdprService(db)
	gdprProcessor := processor.NewGdprProcessor(openaiClient, faceDetectorClient)

	log.Printf("GDPR process service started. Connecting to RabbitMQ...")

	// Test OpenAI integration if API key is available
	testOpenAI()

	// Test database update functionality
	testDatabaseUpdate()

	// Initialize RabbitMQ subscriber
	subscriber, err := rabbitmq.NewSubscriber(cfg.GetRabbitMQURL(), cfg.RabbitMQExchange, cfg.RabbitMQQueue)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ subscriber: %v", err)
	}
	defer subscriber.Close()

	// Define callbacks for different message types
	callbacks := map[string]rabbitmq.CallbackFunc{
		cfg.RabbitMQUserRoutingKey: func(msg *rabbitmq.Message) error {
			return handleUserMessage(msg, gdprService, gdprProcessor)
		},
		cfg.RabbitMQReportRoutingKey: func(msg *rabbitmq.Message) error {
			return handleReportMessage(msg, gdprService, gdprProcessor, cfg.ImagePlaceholderPath)
		},
	}

	// Start consuming messages
	err = subscriber.Start(callbacks)
	if err != nil {
		log.Fatalf("Failed to start RabbitMQ subscriber: %v", err)
	}

	log.Printf("GDPR process service listening for messages on exchange: %s, queue: %s", cfg.RabbitMQExchange, cfg.RabbitMQQueue)

	// Keep the service running
	select {}
}

// handleUserMessage processes a user message from RabbitMQ
func handleUserMessage(msg *rabbitmq.Message, gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor) error {
	var userMsg UserMessage
	if err := json.Unmarshal(msg.Body, &userMsg); err != nil {
		return rabbitmq.Permanent(fmt.Errorf("failed to unmarshal user message: %w", err))
	}

	log.Printf("Processing user message for user ID: %s, avatar: %s", userMsg.Id, userMsg.Avatar)

	// Process the single user with full user data
	result := processSingleUser(gdprService, gdprProcessor, userMsg.Id, userMsg.Avatar)
	if result.err != nil {
		return fmt.Errorf("failed to process user %s: %w", userMsg.Id, result.err)
	}

	log.Printf("Successfully processed user: %s", userMsg.Id)
	return nil
}

// handleReportMessage processes a report message from RabbitMQ
func handleReportMessage(msg *rabbitmq.Message, gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, imagePlaceholderPath string) error {
	var reportMsg ReportMessage
	if err := json.Unmarshal(msg.Body, &reportMsg); err != nil {
		return rabbitmq.Permanent(fmt.Errorf("failed to unmarshal report message: %w", err))
	}

	log.Printf("Processing report message for report seq: %d, description: %s", reportMsg.Seq, reportMsg.Description)

	// Process the single report with full report data
	result := processSingleReport(gdprService, gdprProcessor, reportMsg.Seq, reportMsg.Description, imagePlaceholderPath, 1)
	if result.err != nil {
		return fmt.Errorf("failed to process report %d: %w", reportMsg.Seq, result.err)
	}

	log.Printf("Successfully processed report: %d", reportMsg.Seq)
	return nil
}

// userProcessResult represents the result of processing a single user
type userProcessResult struct {
	userID string
	err    error
}

// processSingleUser processes a single user and returns the result
func processSingleUser(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, userID string, avatar string) userProcessResult {
	// Use the avatar from the message instead of fetching from database
	log.Printf("Processing user %s with avatar: %s", userID, avatar)

	// Process user with OpenAI
	if err := gdprProcessor.ProcessUser(userID, avatar, gdprService.UpdateUserAvatar, gdprService.GenerateUniqueAvatar); err != nil {
		return userProcessResult{userID: userID, err: fmt.Errorf("failed to process user: %w", err)}
	}

	// Mark user as processed
	if err := gdprService.MarkUserProcessed(userID); err != nil {
		return userProcessResult{userID: userID, err: fmt.Errorf("failed to mark user as processed: %w", err)}
	}

	return userProcessResult{userID: userID, err: nil}
}

// reportProcessResult represents the result of processing a single report
type reportProcessResult struct {
	seq int
	err error
}

// processSingleReport processes a single report and returns the result
func processSingleReport(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, seq int, description string, imagePlaceholderPath string, processNumber int) reportProcessResult {
	// Process report with GDPR processor using description from message
	log.Printf("Processing report %d with description: %s", seq, description)

	if err := gdprProcessor.ProcessReport(seq, gdprService.GetReportImage, gdprService.UpdateReportImage, gdprService.GetPlaceholderImage, imagePlaceholderPath, processNumber); err != nil {
		return reportProcessResult{seq: seq, err: fmt.Errorf("failed to process report: %w", err)}
	}

	// Mark report as processed
	if err := gdprService.MarkReportProcessed(seq); err != nil {
		return reportProcessResult{seq: seq, err: fmt.Errorf("failed to mark report as processed: %w", err)}
	}

	return reportProcessResult{seq: seq, err: nil}
}
