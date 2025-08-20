package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"report-ownership-service/config"
	"report-ownership-service/database"
	"report-ownership-service/models"
)

// Service represents the main ownership service
type Service struct {
	cfg     *config.Config
	db      *database.OwnershipService
	ctx     context.Context
	cancel  context.CancelFunc
	stopped chan struct{}
}

// NewService creates a new ownership service instance
func NewService(cfg *config.Config, db *database.OwnershipService) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		cfg:     cfg,
		db:      db,
		ctx:     ctx,
		cancel:  cancel,
		stopped: make(chan struct{}),
	}
}

// Start starts the ownership service
func (s *Service) Start() error {
	log.Println("Starting report ownership service...")

	// Start the ownership processing loop
	go s.processOwnershipLoop()

	log.Println("Report ownership service started successfully")
	return nil
}

// Stop stops the ownership service
func (s *Service) Stop() error {
	log.Println("Stopping report ownership service...")

	// Cancel the context to stop the processing loop
	s.cancel()

	// Wait for the service to stop
	select {
	case <-s.stopped:
		log.Println("Report ownership service stopped successfully")
	case <-time.After(30 * time.Second):
		log.Println("Warning: Service did not stop gracefully within 30 seconds")
	}

	return nil
}

// processOwnershipLoop continuously processes unprocessed reports
func (s *Service) processOwnershipLoop() {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	log.Printf("Starting ownership processing loop with %v interval", s.cfg.PollInterval)

	for {
		select {
		case <-s.ctx.Done():
			log.Println("Ownership processing loop stopped")
			close(s.stopped)
			return
		case <-ticker.C:
			if err := s.processBatch(); err != nil {
				log.Printf("ERROR: Failed to process batch: %v", err)
			}
		}
	}
}

// processBatch processes a batch of unprocessed reports
func (s *Service) processBatch() error {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	// Get unprocessed reports with analysis
	reportsWithAnalysis, err := s.db.GetUnprocessedReports(ctx, s.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get unprocessed reports: %w", err)
	}

	if len(reportsWithAnalysis) == 0 {
		return nil
	}

	log.Printf("Processing %d reports for ownership determination", len(reportsWithAnalysis))

	// Process each report
	for _, reportWithAnalysis := range reportsWithAnalysis {
		if err := s.processReport(ctx, reportWithAnalysis); err != nil {
			log.Printf("ERROR: Failed to process report %d: %v", reportWithAnalysis.Report.Seq, err)
			continue
		}
	}

	log.Printf("Successfully processed %d reports", len(reportsWithAnalysis))
	return nil
}

// processReport determines and stores ownership for a single report
func (s *Service) processReport(ctx context.Context, reportWithAnalysis models.ReportWithAnalysis) error {
	report := reportWithAnalysis.Report
	analysis := reportWithAnalysis.Analysis

	log.Printf("DEBUG: Processing report %d for ownership", report.Seq)

	// Determine owners based on location
	locationOwners, err := s.db.DetermineLocationOwners(ctx, report.Latitude, report.Longitude)
	if err != nil {
		return fmt.Errorf("failed to determine location owners: %w", err)
	}

	// Determine owners based on brand
	var brandOwners []models.OwnerWithPublicFlag
	if analysis.BrandName != "" {
		brandOwners, err = s.db.DetermineBrandOwners(ctx, analysis.BrandName)
		if err != nil {
			return fmt.Errorf("failed to determine brand owners: %w", err)
		}
	}

	// Combine all owners (remove duplicates) and preserve their public flags
	allOwnersMap := make(map[string]bool) // customer_id -> is_public
	for _, owner := range locationOwners {
		allOwnersMap[owner.CustomerID] = owner.IsPublic
	}
	for _, owner := range brandOwners {
		// If customer already exists from location, keep the more restrictive (private) setting
		if existingPublic, exists := allOwnersMap[owner.CustomerID]; exists {
			// If existing is private (false) and new is public (true), keep private
			// If existing is public (true) and new is private (false), update to private
			allOwnersMap[owner.CustomerID] = existingPublic && owner.IsPublic
		} else {
			allOwnersMap[owner.CustomerID] = owner.IsPublic
		}
	}

	// Convert map to separate slices for storage
	var uniqueOwners []string
	var publicFlags []bool
	for customerID, isPublic := range allOwnersMap {
		uniqueOwners = append(uniqueOwners, customerID)
		publicFlags = append(publicFlags, isPublic)
	}

	// Store ownership information
	if err := s.db.StoreReportOwners(ctx, report.Seq, uniqueOwners, publicFlags); err != nil {
		return fmt.Errorf("failed to store report owners: %w", err)
	}

	log.Printf("DEBUG: Report %d has %d owners (location: %d, brand: %d)",
		report.Seq, len(uniqueOwners), len(locationOwners), len(brandOwners))

	return nil
}

// GetStatus returns the current service status
func (s *Service) GetStatus() (*models.ServiceStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lastSeq, err := s.db.GetLastProcessedSeq(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get last processed seq: %w", err)
	}

	totalReports, err := s.db.GetTotalReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get total reports: %w", err)
	}

	status := &models.ServiceStatus{
		Status:           "running",
		LastProcessedSeq: lastSeq,
		TotalReports:     totalReports,
		LastUpdate:       time.Now(),
	}

	return status, nil
}

// GetDatabaseService returns the database service for direct access
func (s *Service) GetDatabaseService() *database.OwnershipService {
	return s.db
}
