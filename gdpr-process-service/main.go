package main

import (
	"fmt"
	"gdpr-process-service/config"
	"gdpr-process-service/database"
	"gdpr-process-service/face_detector"
	"gdpr-process-service/openai"
	"gdpr-process-service/processor"
	"gdpr-process-service/utils"
	"log"
	"sync"
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

	// Initialize face detector client
	faceDetectorClient := face_detector.NewClient(cfg.FaceDetectorURL, cfg.FaceDetectorPortStart)

	// Initialize services
	gdprService := database.NewGdprService(db)
	gdprProcessor := processor.NewGdprProcessor(openaiClient, faceDetectorClient)

	log.Printf("GDPR process service started. Polling every %d seconds...", cfg.PollInterval)

	// Test OpenAI integration if API key is available
	testOpenAI()

	// Test database update functionality
	testDatabaseUpdate()

	// Start the polling loop
	for {
		if err := processBatch(gdprService, gdprProcessor, cfg); err != nil {
			log.Printf("Error processing batch: %v", err)
		}

		// Wait before next polling cycle
		time.Sleep(cfg.PollInterval)
	}
}

// processBatch processes a batch of unprocessed users and reports
func processBatch(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, cfg *config.Config) error {
	// Process users in parallel batches
	userIDs, err := gdprService.GetUnprocessedUsers()
	if err != nil {
		return err
	}

	if len(userIDs) > 0 {
		if err := processUsersInParallel(gdprService, gdprProcessor, userIDs, cfg.BatchSize, cfg.MaxWorkers); err != nil {
			log.Printf("Error processing users in parallel: %v", err)
		}
	}

	// Process reports in parallel batches
	reportSeqs, err := gdprService.GetUnprocessedReports()
	if err != nil {
		return err
	}

	if len(reportSeqs) > 0 {
		if err := processReportsInParallel(gdprService, gdprProcessor, reportSeqs, cfg.BatchSize, cfg.MaxWorkers, cfg.ImagePlaceholderPath); err != nil {
			log.Printf("Error processing reports in parallel: %v", err)
		}
	}

	// Log processing statistics
	if len(userIDs) > 0 || len(reportSeqs) > 0 {
		log.Printf("Processed %d users and %d reports", len(userIDs), len(reportSeqs))
	}

	return nil
}

// processUsersInParallel processes users in parallel batches
func processUsersInParallel(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, userIDs []string, batchSize int, maxWorkers int) error {

	log.Printf("Processing %d users in parallel batches of %d", len(userIDs), batchSize)

	// Create a channel to collect results
	resultChan := make(chan userProcessResult, len(userIDs))

	// Create a semaphore to limit concurrent workers
	semaphore := make(chan struct{}, maxWorkers)

	// Process users in batches
	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}

		batch := userIDs[i:end]
		log.Printf("Processing batch %d-%d (%d users)", i+1, end, len(batch))

		// Process batch concurrently
		var wg sync.WaitGroup
		for _, userID := range batch {
			wg.Add(1)
			go func(uid string) {
				defer wg.Done()

				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Process user
				result := processSingleUser(gdprService, gdprProcessor, uid)
				resultChan <- result
			}(userID)
		}

		// Wait for current batch to complete
		wg.Wait()
	}

	// Close result channel after all goroutines complete
	close(resultChan)

	// Collect and process results
	successCount := 0
	errorCount := 0

	for result := range resultChan {
		if result.err != nil {
			errorCount++
			log.Printf("Failed to process user %s: %v", result.userID, result.err)
		} else {
			successCount++
			log.Printf("Successfully processed user %s", result.userID)
		}
	}

	log.Printf("Batch processing completed: %d successful, %d failed", successCount, errorCount)
	return nil
}

// processReportsInParallel processes reports in parallel batches
func processReportsInParallel(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, reportSeqs []int, batchSize int, maxWorkers int, imagePlaceholderPath string) error {

	log.Printf("Processing %d reports in parallel batches of %d", len(reportSeqs), batchSize)

	// Create a channel to collect results
	resultChan := make(chan reportProcessResult, len(reportSeqs))

	// Create a semaphore to limit concurrent workers
	semaphore := make(chan struct{}, maxWorkers)

	// Process reports in batches
	for i := 0; i < len(reportSeqs); i += batchSize {
		end := min(i + batchSize, len(reportSeqs))

		batch := reportSeqs[i:end]
		log.Printf("Processing batch %d-%d (%d reports)", i+1, end, len(batch))

		// Process batch concurrently
		var wg sync.WaitGroup
		for i, seq := range batch {
			wg.Add(1)
			go func(reportSeq int, processNumber int) {
				defer wg.Done()

				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Process report
				result := processSingleReport(gdprService, gdprProcessor, reportSeq, imagePlaceholderPath, processNumber)
				resultChan <- result
			}(seq, i+1)
		}

		// Wait for current batch to complete
		wg.Wait()
	}

	// Close result channel after all goroutines complete
	close(resultChan)

	// Collect and process results
	successCount := 0
	errorCount := 0

	for result := range resultChan {
		if result.err != nil {
			errorCount++
			log.Printf("Failed to process report %d: %v", result.seq, result.err)
		} else {
			successCount++
			log.Printf("Successfully processed report %d", result.seq)
		}
	}

	log.Printf("Report batch processing completed: %d successful, %d failed", successCount, errorCount)
	return nil
}

// userProcessResult represents the result of processing a single user
type userProcessResult struct {
	userID string
	err    error
}

// processSingleUser processes a single user and returns the result
func processSingleUser(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, userID string) userProcessResult {
	// Fetch user avatar data
	avatar, err := gdprService.GetUserData(userID)
	if err != nil {
		return userProcessResult{userID: userID, err: fmt.Errorf("failed to fetch user data: %w", err)}
	}

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
func processSingleReport(gdprService *database.GdprService, gdprProcessor *processor.GdprProcessor, seq int, imagePlaceholderPath string, processNumber int) reportProcessResult {
	// Process report with GDPR processor
	if err := gdprProcessor.ProcessReport(seq, gdprService.GetReportImage, gdprService.UpdateReportImage, gdprService.GetPlaceholderImage, imagePlaceholderPath, processNumber); err != nil {
		return reportProcessResult{seq: seq, err: fmt.Errorf("failed to process report: %w", err)}
	}

	// Mark report as processed
	if err := gdprService.MarkReportProcessed(seq); err != nil {
		return reportProcessResult{seq: seq, err: fmt.Errorf("failed to mark report as processed: %w", err)}
	}

	return reportProcessResult{seq: seq, err: nil}
}
