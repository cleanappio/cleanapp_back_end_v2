package services

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"montenegro-areas/models"
	ws "montenegro-areas/websocket"
)

// WebSocketService manages the WebSocket broadcasting for Montenegro reports
type WebSocketService struct {
	db           *DatabaseService
	areasService *AreasService
	hub          *ws.Hub

	// Montenegro area (admin_level 2, osm_id -53296)
	montenegroArea *models.MontenegroArea

	// State tracking
	lastProcessedSeq int
	mu               sync.RWMutex

	// Control channels
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewWebSocketService creates a new WebSocket service
func NewWebSocketService(dbService *DatabaseService, areasService *AreasService) (*WebSocketService, error) {
	// Initialize WebSocket hub
	hub := ws.NewHub()

	service := &WebSocketService{
		db:           dbService,
		areasService: areasService,
		hub:          hub,
		stopChan:     make(chan struct{}),
	}

	return service, nil
}

// Start starts the WebSocket service
func (s *WebSocketService) Start() error {
	log.Printf("Starting Montenegro WebSocket service...")

	// Start the WebSocket hub
	go s.hub.Run()

	// Find Montenegro area (admin_level 2, osm_id -53296)
	if err := s.findMontenegroArea(); err != nil {
		return fmt.Errorf("failed to find Montenegro area: %w", err)
	}

	// Initialize last processed sequence
	if err := s.initializeLastProcessedSeq(); err != nil {
		return fmt.Errorf("failed to initialize last processed seq: %w", err)
	}

	// Start the broadcast loop
	s.wg.Add(1)
	go s.broadcastLoop()

	log.Printf("Montenegro WebSocket service started successfully")
	return nil
}

// Stop stops the WebSocket service gracefully
func (s *WebSocketService) Stop() error {
	log.Printf("Stopping Montenegro WebSocket service...")

	// Signal stop
	close(s.stopChan)

	// Wait for goroutines to finish
	s.wg.Wait()

	log.Printf("Montenegro WebSocket service stopped")
	return nil
}

// GetHub returns the WebSocket hub
func (s *WebSocketService) GetHub() *ws.Hub {
	return s.hub
}

// GetStats returns current service statistics
func (s *WebSocketService) GetStats() (int, int, int) {
	connectedClients, lastBroadcastSeq := s.hub.GetStats()
	s.mu.RLock()
	lastProcessedSeq := s.lastProcessedSeq
	s.mu.RUnlock()
	return connectedClients, lastBroadcastSeq, lastProcessedSeq
}

// findMontenegroArea finds the Montenegro area (admin_level 2, osm_id -53296)
func (s *WebSocketService) findMontenegroArea() error {
	areas, err := s.areasService.GetAreasByAdminLevel(2)
	if err != nil {
		return fmt.Errorf("failed to get areas for admin level 2: %w", err)
	}

	for _, area := range areas {
		if area.OSMID == -53296 {
			s.montenegroArea = &area
			log.Printf("Found Montenegro area: %s (OSM ID: %d)", area.Name, area.OSMID)
			return nil
		}
	}

	return fmt.Errorf("Montenegro area (OSM ID: -53296) not found")
}

// initializeLastProcessedSeq initializes the last processed sequence number
func (s *WebSocketService) initializeLastProcessedSeq() error {
	// Get the latest sequence from reports table
	latestSeq, err := s.getLatestReportSeq()
	if err != nil {
		return fmt.Errorf("failed to get latest report seq: %w", err)
	}

	s.mu.Lock()
	s.lastProcessedSeq = latestSeq
	s.mu.Unlock()

	log.Printf("Initialized last processed sequence: %d", latestSeq)
	return nil
}

// getLatestReportSeq returns the latest sequence number from the reports table
func (s *WebSocketService) getLatestReportSeq() (int, error) {
	var seq int
	err := s.db.db.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM reports").Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest report seq: %w", err)
	}
	return seq, nil
}

// broadcastLoop continuously polls for new reports in Montenegro and broadcasts them
func (s *WebSocketService) broadcastLoop() {
	defer s.wg.Done()

	// Poll every 5 seconds for new reports
	ticker := time.NewTicker(5 * time.Second)
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

// processNewReports fetches and broadcasts new reports in Montenegro
func (s *WebSocketService) processNewReports() error {
	s.mu.RLock()
	lastSeq := s.lastProcessedSeq
	s.mu.RUnlock()

	// Fetch new reports in Montenegro
	reports, err := s.getReportsSince(lastSeq)
	if err != nil {
		return err
	}

	if len(reports) == 0 {
		return nil
	}

	// Broadcast reports
	s.hub.BroadcastReports(reports)

	// Update last processed sequence in memory
	newLastSeq := reports[len(reports)-1].Seq

	s.mu.Lock()
	s.lastProcessedSeq = newLastSeq
	s.mu.Unlock()

	log.Printf("Processed %d new reports in Montenegro (seq %d-%d)",
		len(reports), reports[0].Seq, reports[len(reports)-1].Seq)

	return nil
}

// getReportsSince retrieves reports in Montenegro since a given sequence number
func (s *WebSocketService) getReportsSince(sinceSeq int) ([]models.ReportData, error) {
	if s.montenegroArea == nil {
		return nil, fmt.Errorf("Montenegro area not found")
	}

	// Convert the Montenegro area geometry to WKT format
	areaWKT, err := s.db.wktConverter.ConvertGeoJSONToWKT(s.montenegroArea.Area)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Montenegro area geometry to WKT: %w", err)
	}

	// Query to get new reports within Montenegro
	query := `
		SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id
		FROM reports r
		JOIN reports_geometry rg ON r.seq = rg.seq
		WHERE r.seq > ? AND ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		ORDER BY r.seq ASC
	`

	rows, err := s.db.db.Query(query, sinceSeq, areaWKT)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports in Montenegro: %w", err)
	}
	defer rows.Close()

	var reports []models.ReportData
	for rows.Next() {
		var report models.ReportData
		var timestamp time.Time
		var x, y sql.NullFloat64
		var actionID sql.NullString

		err := rows.Scan(
			&report.Seq,
			&timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&x,
			&y,
			&actionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		// Convert timestamp to string
		report.Timestamp = timestamp.Format(time.RFC3339)

		// Handle nullable fields
		if x.Valid {
			report.X = &x.Float64
		}
		if y.Valid {
			report.Y = &y.Float64
		}
		if actionID.Valid {
			report.ActionID = &actionID.String
		}

		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	return reports, nil
}
