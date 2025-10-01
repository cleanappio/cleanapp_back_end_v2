package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"report-listener/config"
	"report-listener/database"
	"report-listener/handlers"
	"report-listener/websocket"
)

// Service manages the report listening and broadcasting
type Service struct {
	config   *config.Config
	db       *database.Database
	hub      *websocket.Hub
	handlers *handlers.Handlers

	// State tracking
	lastProcessedSeq int
	mu               sync.RWMutex

	// Control channels
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewService creates a new report listener service
func NewService(cfg *config.Config) (*Service, error) {
	// Initialize database
	db, err := database.NewDatabase(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize WebSocket hub
	hub := websocket.NewHub()

	// Initialize handlers
	handlers := handlers.NewHandlers(hub, db)

	service := &Service{
		config:   cfg,
		db:       db,
		hub:      hub,
		handlers: handlers,
		stopChan: make(chan struct{}),
	}

	return service, nil
}

// Start starts the service
func (s *Service) Start() error {
	log.Printf("Starting report listener service...")

	// Ensure tables for bulk ingest
	if err := s.db.EnsureFetcherTables(context.Background()); err != nil {
		return err
	}

	// Ensure utf8mb4 for Unicode content
	if err := s.db.EnsureUTF8MB4(context.Background()); err != nil {
		log.Printf("Warning: UTF8MB4 ensure failed: %v", err)
	}

	// Start the WebSocket hub
	go s.hub.Run()

	// Initialize last processed sequence
	if err := s.initializeLastProcessedSeq(); err != nil {
		return err
	}

	// Start the broadcast loop
	s.wg.Add(1)
	go s.broadcastLoop()

	log.Printf("Report listener service started successfully")
	return nil
}

// Stop stops the service gracefully
func (s *Service) Stop() error {
	log.Printf("Stopping report listener service...")

	// Signal stop
	close(s.stopChan)

	// Wait for goroutines to finish
	s.wg.Wait()

	// Close database connection
	if err := s.db.Close(); err != nil {
		log.Printf("Error closing database: %v", err)
	}

	log.Printf("Report listener service stopped")
	return nil
}

// GetHandlers returns the HTTP handlers
func (s *Service) GetHandlers() *handlers.Handlers {
	return s.handlers
}

// GetStats returns current service statistics
func (s *Service) GetStats() (int, int, int) {
	connectedClients, lastBroadcastSeq := s.hub.GetStats()
	s.mu.RLock()
	lastProcessedSeq := s.lastProcessedSeq
	s.mu.RUnlock()
	return connectedClients, lastBroadcastSeq, lastProcessedSeq
}

// initializeLastProcessedSeq initializes the last processed sequence number
func (s *Service) initializeLastProcessedSeq() error {
	ctx := context.Background()

	// Ensure the service_state table exists
	if err := s.db.EnsureServiceStateTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure service_state table: %w", err)
	}

	// Try to get the last processed sequence from persistent storage
	lastSeq, err := s.db.GetLastProcessedSeq(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last processed seq: %w", err)
	}

	// If no persistent state exists, get the latest sequence from reports table
	if lastSeq == 0 {
		latestSeq, err := s.db.GetLatestReportSeq(ctx)
		if err != nil {
			return fmt.Errorf("failed to get latest report seq: %w", err)
		}
		lastSeq = latestSeq

		// Store the initial state
		if err := s.db.UpdateLastProcessedSeq(ctx, lastSeq); err != nil {
			log.Printf("Warning: failed to store initial sequence state: %v", err)
		}
	}

	s.mu.Lock()
	s.lastProcessedSeq = lastSeq
	s.mu.Unlock()

	log.Printf("Initialized last processed sequence: %d", lastSeq)
	return nil
}

// broadcastLoop continuously polls for new reports and broadcasts them
func (s *Service) broadcastLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.BroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			if err := s.processNewReports(); err != nil {
				log.Printf("Error processing new reports: %v", err)
			}
		}
	}
}

// processNewReports fetches and broadcasts new reports
func (s *Service) processNewReports() error {
	ctx := context.Background()

	s.mu.RLock()
	lastSeq := s.lastProcessedSeq
	s.mu.RUnlock()

	// Fetch new reports with analysis
	reportsWithAnalysis, err := s.db.GetReportsSince(ctx, lastSeq)
	if err != nil {
		return err
	}

	if len(reportsWithAnalysis) == 0 {
		return nil
	}

	// Broadcast reports
	s.hub.BroadcastReports(reportsWithAnalysis)

	// Update last processed sequence in memory and persistent storage
	newLastSeq := reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq

	s.mu.Lock()
	s.lastProcessedSeq = newLastSeq
	s.mu.Unlock()

	// Persist the updated sequence number
	if err := s.db.UpdateLastProcessedSeq(ctx, newLastSeq); err != nil {
		log.Printf("Warning: failed to persist last processed seq: %v", err)
		// Don't return error here as the broadcast was successful
	}

	log.Printf("Processed %d new reports with analysis (seq %d-%d)",
		len(reportsWithAnalysis), reportsWithAnalysis[0].Report.Seq, reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq)

	return nil
}
