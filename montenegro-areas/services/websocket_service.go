package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
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

	return fmt.Errorf("montenegro area (OSM ID: -53296) not found")
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

// processNewReports fetches and broadcasts new reports with analysis in Montenegro
func (s *WebSocketService) processNewReports() error {
	s.mu.RLock()
	lastSeq := s.lastProcessedSeq
	s.mu.RUnlock()

	// Fetch new reports with analysis in Montenegro
	reports, err := s.getReportsWithAnalysisSince(lastSeq)
	if err != nil {
		return err
	}

	if len(reports) == 0 {
		return nil
	}

	// Broadcast reports
	s.hub.BroadcastReports(reports)

	// Update last processed sequence in memory
	newLastSeq := reports[len(reports)-1].Report.Seq

	s.mu.Lock()
	s.lastProcessedSeq = newLastSeq
	s.mu.Unlock()

	log.Printf("Processed %d new reports with analysis in Montenegro (seq %d-%d)",
		len(reports), reports[0].Report.Seq, reports[len(reports)-1].Report.Seq)

	return nil
}

// getReportsWithAnalysisSince retrieves reports with analysis in Montenegro since a given sequence number
func (s *WebSocketService) getReportsWithAnalysisSince(sinceSeq int) ([]models.ReportWithAnalysis, error) {
	if s.montenegroArea == nil {
		return nil, fmt.Errorf("montenegro area not found")
	}

	// Convert the Montenegro area geometry to WKT format
	areaWKT, err := s.db.wktConverter.ConvertGeoJSONToWKT(s.montenegroArea.Area)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Montenegro area geometry to WKT: %w", err)
	}

	// First, get all reports within Montenegro since the given sequence
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id
		FROM reports r
		JOIN reports_geometry rg ON r.seq = rg.seq
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		WHERE r.seq > ? AND ST_Within(rg.geom, ST_GeomFromText(?, 4326))
		ORDER BY r.seq ASC
	`

	reportRows, err := s.db.db.Query(reportsQuery, sinceSeq, areaWKT)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports in Montenegro: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences and reports
	var reportSeqs []int
	var reports []models.ReportData
	for reportRows.Next() {
		var report models.ReportData
		var timestamp time.Time
		var x, y sql.NullFloat64
		var actionID sql.NullString

		err := reportRows.Scan(
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
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq ASC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := s.db.db.Query(analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		var analysisCreatedAt time.Time
		var analysisImage sql.NullString // Handle nullable analysis_image field

		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysisCreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}

		// Convert timestamp to string
		analysis.CreatedAt = analysisCreatedAt.Format(time.RFC3339)

		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
}
