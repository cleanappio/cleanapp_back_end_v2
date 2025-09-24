package service

import (
	"log"
	"time"

	"report-analysis-backfill/config"
	"report-analysis-backfill/database"
)

// BackfillService handles the backfill process
type BackfillService struct {
	config         *config.Config
	db             *database.Database
	analysisClient *AnalysisClient
	running        bool
	stopChan       chan struct{}
}

// NewBackfillService creates a new backfill service
func NewBackfillService(cfg *config.Config, db *database.Database) *BackfillService {
	return &BackfillService{
		config:         cfg,
		db:             db,
		analysisClient: NewAnalysisClient(cfg, db),
		running:        false,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the backfill service
func (s *BackfillService) Start() {
	if s.running {
		log.Println("Backfill service is already running")
		return
	}

	s.running = true
	log.Printf("Starting backfill service with poll interval: %v, batch size: %d",
		s.config.PollInterval, s.config.BatchSize)

	go s.run()
}

// Stop stops the backfill service
func (s *BackfillService) Stop() {
	if !s.running {
		return
	}

	log.Println("Stopping backfill service...")
	s.running = false
	close(s.stopChan)
}

// run is the main backfill loop
func (s *BackfillService) run() {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			log.Println("Backfill service stopped")
			return
		case <-ticker.C:
			s.processBatch()
		}
	}
}

// processBatch processes a batch of unanalyzed reports
func (s *BackfillService) processBatch() {
	log.Printf("Processing batch of up to %d reports...", s.config.BatchSize)

	// Get unanalyzed reports
	reports, err := s.db.GetUnanalyzedReports(s.config, s.config.BatchSize)
	if err != nil {
		log.Printf("Failed to get unanalyzed reports: %v", err)
		return
	}

	if len(reports) == 0 {
		log.Println("No unanalyzed reports found")
		return
	}

	log.Printf("Found %d unanalyzed reports, sending to analysis API...", len(reports))

	// Send reports to analysis API
	if err := s.analysisClient.SendReportsBatch(reports); err != nil {
		log.Printf("Failed to send reports batch: %v", err)
		return
	}

	log.Printf("Successfully processed batch of %d reports", len(reports))
}

// IsRunning returns whether the service is currently running
func (s *BackfillService) IsRunning() bool {
	return s.running
}

// GetStats returns current statistics
func (s *BackfillService) GetStats() map[string]interface{} {
	lastSeq, err := s.db.GetLastProcessedSeq()
	if err != nil {
		log.Printf("Failed to get last processed seq: %v", err)
		lastSeq = 0
	}

	return map[string]interface{}{
		"running":            s.running,
		"poll_interval":      s.config.PollInterval.String(),
		"batch_size":         s.config.BatchSize,
		"last_processed_seq": lastSeq,
		"end_to_seq":         s.config.SeqEndTo,
	}
}
