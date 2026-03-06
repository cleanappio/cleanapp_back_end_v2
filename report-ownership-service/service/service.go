package service

import (
	"cleanapp-common/events"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"report-ownership-service/config"
	"report-ownership-service/database"
	"report-ownership-service/models"
	"report-ownership-service/rabbitmq"
)

// Service represents the main ownership service
type Service struct {
	cfg        *config.Config
	db         *database.OwnershipService
	subscriber *rabbitmq.Subscriber
	ctx        context.Context
	cancel     context.CancelFunc
	stopped    chan struct{}
}

// NewService creates a new ownership service instance
func NewService(cfg *config.Config, db *database.OwnershipService) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create RabbitMQ subscriber
	subscriber, err := rabbitmq.NewSubscriber(
		cfg.GetRabbitMQURL(),
		cfg.RabbitMQExchange,
		cfg.RabbitMQQueue,
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create RabbitMQ subscriber: %w", err)
	}

	return &Service{
		cfg:        cfg,
		db:         db,
		subscriber: subscriber,
		ctx:        ctx,
		cancel:     cancel,
		stopped:    make(chan struct{}),
	}, nil
}

// Start starts the ownership service
func (s *Service) Start() error {
	log.Println("Starting report ownership service...")

	// Start the RabbitMQ subscription
	go s.startRabbitMQSubscription()

	log.Println("Report ownership service started successfully")
	return nil
}

// Stop stops the ownership service
func (s *Service) Stop() error {
	log.Println("Stopping report ownership service...")

	// Cancel the context to stop the processing
	s.cancel()

	// Close RabbitMQ subscriber
	if s.subscriber != nil {
		if err := s.subscriber.Close(); err != nil {
			log.Printf("Error closing RabbitMQ subscriber: %v", err)
		}
	}

	// Wait for the service to stop
	select {
	case <-s.stopped:
		log.Println("Report ownership service stopped successfully")
	case <-time.After(30 * time.Second):
		log.Println("Warning: Service did not stop gracefully within 30 seconds")
	}

	return nil
}

// handleReportMessage processes a single report message from RabbitMQ
func (s *Service) handleReportMessage(msg *rabbitmq.Message) error {
	reportWithAnalysis, err := s.decodeReportMessage(msg)
	if err != nil {
		return err
	}

	log.Printf("Received report %d for ownership processing", reportWithAnalysis.Report.Seq)

	// Process the report
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	if err := s.processReport(ctx, *reportWithAnalysis); err != nil {
		return fmt.Errorf("failed to process report %d: %w", reportWithAnalysis.Report.Seq, err)
	}

	log.Printf("Successfully processed report %d for ownership", reportWithAnalysis.Report.Seq)
	return nil
}

func (s *Service) decodeReportMessage(msg *rabbitmq.Message) (*models.ReportWithAnalysis, error) {
	if event, err := events.DecodeReportAnalysed(msg.Body); err == nil && event.Report.Seq > 0 {
		return convertAnalysedEvent(event), nil
	}

	var reportWithAnalysis models.ReportWithAnalysis
	if err := msg.UnmarshalTo(&reportWithAnalysis); err == nil && reportWithAnalysis.Report.Seq > 0 {
		return &reportWithAnalysis, nil
	}

	type seqEnvelope struct {
		Seq int `json:"seq"`
	}

	var seq int
	if err := json.Unmarshal(msg.Body, &seq); err == nil && seq > 0 {
		return s.loadReportWithAnalysis(seq)
	}

	var envelope seqEnvelope
	if err := json.Unmarshal(msg.Body, &envelope); err == nil && envelope.Seq > 0 {
		return s.loadReportWithAnalysis(envelope.Seq)
	}

	return nil, rabbitmq.Permanent(fmt.Errorf("failed to decode report ownership message body: %s", string(msg.Body)))
}

func (s *Service) loadReportWithAnalysis(seq int) (*models.ReportWithAnalysis, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	reportWithAnalysis, err := s.db.GetReportWithAnalysis(ctx, seq)
	if err != nil {
		return nil, fmt.Errorf("load report with analysis seq=%d: %w", seq, err)
	}
	return reportWithAnalysis, nil
}

func convertAnalysedEvent(event *events.ReportAnalysed) *models.ReportWithAnalysis {
	if event == nil {
		return nil
	}
	converted := &models.ReportWithAnalysis{
		Report: models.Report{
			Seq:         event.Report.Seq,
			Timestamp:   event.Report.Timestamp,
			ID:          event.Report.ID,
			Team:        event.Report.Team,
			Latitude:    event.Report.Latitude,
			Longitude:   event.Report.Longitude,
			X:           event.Report.X,
			Y:           event.Report.Y,
			ActionID:    event.Report.ActionID,
			Description: event.Report.Description,
		},
	}
	for _, analysis := range event.Analysis {
		converted.Analysis = append(converted.Analysis, models.ReportAnalysis{
			Seq:                   analysis.Seq,
			Source:                analysis.Source,
			AnalysisText:          analysis.AnalysisText,
			Title:                 analysis.Title,
			Description:           analysis.Description,
			BrandName:             analysis.BrandName,
			BrandDisplayName:      analysis.BrandDisplayName,
			LitterProbability:     analysis.LitterProbability,
			HazardProbability:     analysis.HazardProbability,
			DigitalBugProbability: analysis.DigitalBugProbability,
			SeverityLevel:         analysis.SeverityLevel,
			Summary:               analysis.Summary,
			Language:              analysis.Language,
			Classification:        analysis.Classification,
			IsValid:               analysis.IsValid,
			InferredContactEmails: analysis.InferredContactEmails,
			CreatedAt:             analysis.CreatedAt,
			UpdatedAt:             analysis.UpdatedAt,
		})
	}
	return converted
}

// startRabbitMQSubscription starts the RabbitMQ subscription for report processing
func (s *Service) startRabbitMQSubscription() {
	defer close(s.stopped)

	log.Printf("Starting RabbitMQ subscription for routing key: %s",
		s.cfg.RabbitMQAnalysedReportRoutingKey)

	// Set up routing key callbacks
	routingKeyCallbacks := map[string]rabbitmq.CallbackFunc{
		s.cfg.RabbitMQAnalysedReportRoutingKey: s.handleReportMessage,
	}

	// Start the subscriber
	if err := s.subscriber.Start(routingKeyCallbacks); err != nil {
		log.Printf("ERROR: Failed to start RabbitMQ subscription: %v", err)
		return
	}

	log.Println("RabbitMQ subscription started successfully")

	// Wait for context cancellation
	<-s.ctx.Done()
	log.Println("RabbitMQ subscription stopped")
}

// processReport determines and stores ownership for a single report
func (s *Service) processReport(ctx context.Context, reportWithAnalysis models.ReportWithAnalysis) error {
	report := reportWithAnalysis.Report
	analysis := reportWithAnalysis.Analysis[0]
	for _, an := range reportWithAnalysis.Analysis {
		if an.Language == "en" {
			analysis = an
			break
		}
	}

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
