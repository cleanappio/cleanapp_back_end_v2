package main

import (
	"gdpr-process-service/config"
	"gdpr-process-service/database"
	"gdpr-process-service/openai"
	"gdpr-process-service/processor"
	"gdpr-process-service/utils"
	"log"
	"time"
)

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

	// Initialize services
	gdprService := database.NewGdprService(db)
	gdprProcessor := processor.NewGdprProcessor(openaiClient)

	log.Printf("GDPR process service started. Polling every %d seconds...", cfg.PollInterval)

	// Test OpenAI integration if API key is available
	testOpenAI()

	// Test database update functionality
	testDatabaseUpdate()

	// Start the polling loop
	for {
		if err := processBatch(gdprService, gdprProcessor); err != nil {
			log.Printf("Error processing batch: %v", err)
		}

		// Wait before next polling cycle
		time.Sleep(time.Duration(cfg.PollInterval) * time.Second)
	}
}

// processBatch processes a batch of unprocessed users and reports
func processBatch(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor) error {
	// Process users
	userIDs, err := gdprService.GetUnprocessedUsers()
	if err != nil {
		return err
	}

	for _, userID := range userIDs {
		// Fetch user avatar data
		avatar, err := gdprService.GetUserData(userID)
		if err != nil {
			log.Printf("Failed to fetch user data for %s: %v", userID, err)
			continue
		}

		if err := gdprProcessor.ProcessUser(userID, avatar, gdprService.UpdateUserAvatar); err != nil {
			log.Printf("Failed to process user %s: %v", userID, err)
			continue
		}

		if err := gdprService.MarkUserProcessed(userID); err != nil {
			log.Printf("Failed to mark user %s as processed: %v", userID, err)
			continue
		}
	}

	// Process reports
	reportSeqs, err := gdprService.GetUnprocessedReports()
	if err != nil {
		return err
	}

	for _, seq := range reportSeqs {
		if err := gdprProcessor.ProcessReport(seq); err != nil {
			log.Printf("Failed to process report %d: %v", seq, err)
			continue
		}

		if err := gdprService.MarkReportProcessed(seq); err != nil {
			log.Printf("Failed to mark report %d as processed: %v", seq, err)
			continue
		}
	}

	// Log processing statistics
	if len(userIDs) > 0 || len(reportSeqs) > 0 {
		log.Printf("Processed %d users and %d reports", len(userIDs), len(reportSeqs))
	}

	return nil
}
