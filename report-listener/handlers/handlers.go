package handlers

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"report-listener/database"
	"report-listener/models"
	brandutil "report-listener/utils"
	ws "report-listener/websocket"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
)

const (
	// MaxReportsLimit is the maximum number of reports that can be requested in a single query
	MaxReportsLimit = 100000
)

// Handlers contains all HTTP handlers
type Handlers struct {
	hub *ws.Hub
	db  *database.Database
}

// NewHandlers creates a new handlers instance
func NewHandlers(hub *ws.Hub, db *database.Database) *Handlers {
	return &Handlers{
		hub: hub,
		db:  db,
	}
}

// DB exposes the underlying database handle for wiring
func (h *Handlers) Db() *database.Database {
	return h.db
}

// BulkIngestRequest is the payload schema for bulk ingest
type BulkIngestRequest struct {
	Source string `json:"source"`
	Items  []struct {
		ExternalID string                 `json:"external_id"`
		Title      string                 `json:"title"`
		Content    string                 `json:"content"`
		URL        string                 `json:"url"`
		CreatedAt  string                 `json:"created_at"`
		UpdatedAt  string                 `json:"updated_at"`
		Score      float64                `json:"score"`
		Metadata   map[string]interface{} `json:"metadata"`
		SkipAI     *bool                  `json:"skip_ai"`
	} `json:"items"`
}

// BulkIngestResponse contains per-batch stats
type BulkIngestResponse struct {
	Inserted int         `json:"inserted"`
	Updated  int         `json:"updated"`
	Skipped  int         `json:"skipped"`
	Errors   []BulkError `json:"errors"`
}

type BulkError struct {
	Index  int    `json:"i"`
	Reason string `json:"reason"`
}

// BulkIngest handles POST /api/v3/reports/bulk_ingest
func (h *Handlers) BulkIngest(c *gin.Context) {
	var req BulkIngestRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.Source == "" || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source and items required"})
		return
	}

	fetcherID := c.GetString("fetcher_id")
	if fetcherID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Simple limits
	if len(req.Items) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items limit 1000"})
		return
	}

	resp := BulkIngestResponse{}

	for i, it := range req.Items {
		if it.ExternalID == "" {
			resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "missing external_id"})
			resp.Skipped++
			continue
		}

		// Idempotency: check mapping
		seq, exists, err := h.db.GetSeqByExternal(c.Request.Context(), req.Source, it.ExternalID)
		if err != nil {
			resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "db error"})
			continue
		}

		if !exists {
			// Insert into reports with minimal required fields
			// Using backend SaveReport would require image; we insert directly here with empty image
			insertReport := `INSERT INTO reports (id, team, action_id, latitude, longitude, x, y, image, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
			reporterID := "fetcher:" + fetcherID
			// Digital placeholder
			_, err := h.db.DB().ExecContext(c.Request.Context(), insertReport,
				reporterID, 0, "", 0.0, 0.0, 0.0, 0.0, []byte{}, truncate(it.Title, 255),
			)
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "insert report failed"})
				continue
			}
			// Get seq
			if err := h.db.DB().QueryRowContext(c.Request.Context(), `SELECT MAX(seq) FROM reports`).Scan(&seq); err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "get seq failed"})
				continue
			}

			// Prepare analysis fields per spec
			title := truncate(it.Title, 500)
			description := truncate(it.Content, 65535)
			brandDisplay := extractBrandDisplay(it.Title, it.Metadata)
			brandName := brandutil.NormalizeBrandName(brandDisplay)
			severity := clampSeverity(it.Score)

			// analysis_text empty, image null, litter=0, hazard=0, digital_bug_probability=1
			_, err = h.db.DB().ExecContext(c.Request.Context(), `
                INSERT INTO report_analysis (
                    seq, source, analysis_text, analysis_image, title, description,
                    brand_name, brand_display_name, litter_probability, hazard_probability,
                    digital_bug_probability, severity_level, summary, language, is_valid, classification
                ) VALUES (?, ?, '', NULL, ?, ?, ?, ?, 0.0, 0.0, 1.0, ?, ?, 'en', TRUE, 'digital')
            `, seq, req.Source, title, description, brandName, brandDisplay, severity, truncate(summary(title, description), 65535))
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "insert analysis failed"})
				continue
			}

			if err := h.db.UpsertExternalIngestIndex(c.Request.Context(), req.Source, it.ExternalID, seq); err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "upsert mapping failed"})
				continue
			}
			resp.Inserted++
		} else {
			// Optional update of analysis if changed
			title := truncate(it.Title, 500)
			description := truncate(it.Content, 65535)
			brandDisplay := extractBrandDisplay(it.Title, it.Metadata)
			brandName := brandutil.NormalizeBrandName(brandDisplay)
			severity := clampSeverity(it.Score)

			_, err = h.db.DB().ExecContext(c.Request.Context(), `
                UPDATE report_analysis SET title = ?, description = ?, brand_name = ?, brand_display_name = ?,
                    severity_level = ?, summary = ?, updated_at = NOW()
                WHERE seq = ?
            `, title, description, brandName, brandDisplay, severity, truncate(summary(title, description), 65535), seq)
			if err != nil {
				resp.Errors = append(resp.Errors, BulkError{Index: i, Reason: "update analysis failed"})
				continue
			}
			resp.Updated++
		}
	}

	c.JSON(http.StatusOK, resp)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// naive byte trim; DB columns are VARCHAR/TEXT; keep simple here
	return s[:max]
}

func summary(title, desc string) string {
	if title == "" {
		return truncate(desc, 256)
	}
	if desc == "" {
		return title
	}
	return title + ": " + truncate(desc, 256)
}

func extractBrandDisplay(title string, metadata map[string]interface{}) string {
	if metadata != nil {
		if v, ok := metadata["app_name"].(string); ok && v != "" {
			return v
		}
		if v, ok := metadata["repo_full_name"].(string); ok && v != "" {
			return v
		}
	}
	return title
}

func clampSeverity(score float64) float64 {
	// Accept pre-normalized [0.0..1.0], clamp to [0.7..1.0]
	if score < 0.7 {
		return 0.7
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

// WebSocket upgrader
var upgrader = gorilla.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now
		// In production, you should implement proper origin checking
		return true
	},
}

// ListenReports handles WebSocket connections for report listening
func (h *Handlers) ListenReports(c *gin.Context) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection to WebSocket: %v", err)
		return
	}

	// Create a new client
	client := ws.NewClient(h.hub, conn)

	// Register the client with the hub
	h.hub.Register <- client

	// Start the client's read and write pumps in goroutines
	go client.WritePump()
	go client.ReadPump()

	log.Printf("WebSocket connection established")
}

// HealthCheck returns the service health status
func (h *Handlers) HealthCheck(c *gin.Context) {
	connectedClients, lastBroadcastSeq := h.hub.GetStats()

	response := models.HealthResponse{
		Status:           "healthy",
		Service:          "report-listener",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ConnectedClients: connectedClients,
		LastBroadcastSeq: lastBroadcastSeq,
	}

	c.JSON(http.StatusOK, response)
}

// GetLastNAnalyzedReports returns the last N analyzed reports
func (h *Handlers) GetLastNAnalyzedReports(c *gin.Context) {
	// Get the limit parameter from query string, default to 10 if not provided
	limitStr := c.DefaultQuery("n", "10")
	classification := c.DefaultQuery("classification", "physical")

	limit := 10 // default value
	if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
		limit = parsedLimit
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum number of reports to prevent abuse
	if limit > MaxReportsLimit {
		limit = MaxReportsLimit
	}

	// Get the full_data parameter from query string, default to true if not provided
	fullDataStr := c.DefaultQuery("full_data", "true")
	fullData := true // default value
	if parsedFullData, err := strconv.ParseBool(fullDataStr); err == nil {
		fullData = parsedFullData
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'full_data' parameter. Must be 'true' or 'false'."})
		return
	}

	// Get the reports from the database
	reportsInterface, err := h.db.GetLastNAnalyzedReports(c.Request.Context(), limit, classification, fullData)
	if err != nil {
		log.Printf("Failed to get last N analyzed reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Handle different return types based on full_data parameter
	if fullData {
		// Type assertion to get the reports with analysis
		reportsWithAnalysis, ok := reportsInterface.([]models.ReportWithAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create the response in the same format as WebSocket broadcasts
		response := models.ReportBatch{
			Reports: reportsWithAnalysis,
			Count:   len(reportsWithAnalysis),
			FromSeq: 0,
			ToSeq:   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithAnalysis) > 0 {
			response.FromSeq = reportsWithAnalysis[0].Report.Seq
			response.ToSeq = reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	} else {
		// Type assertion to get reports with minimal analysis
		reportsWithMinimalAnalysis, ok := reportsInterface.([]models.ReportWithMinimalAnalysis)
		if !ok {
			log.Printf("Failed to type assert reports to []models.ReportWithMinimalAnalysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
			return
		}

		// Create a custom response structure for minimal analysis to maintain consistency
		// but with the minimal data structure
		response := gin.H{
			"reports":  reportsWithMinimalAnalysis,
			"count":    len(reportsWithMinimalAnalysis),
			"from_seq": 0,
			"to_seq":   0,
		}

		// Set FromSeq and ToSeq if there are reports
		if len(reportsWithMinimalAnalysis) > 0 {
			response["from_seq"] = reportsWithMinimalAnalysis[0].Report.Seq
			response["to_seq"] = reportsWithMinimalAnalysis[len(reportsWithMinimalAnalysis)-1].Report.Seq
		}

		c.JSON(http.StatusOK, response)
	}
}

// GetReportBySeq returns a specific report by sequence ID
func (h *Handlers) GetReportBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'seq' parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'seq' parameter. Must be a positive integer."})
		return
	}

	// Get the report from the database
	reportWithAnalysis, err := h.db.GetReportBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get report by seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve report"})
		return
	}

	c.JSON(http.StatusOK, reportWithAnalysis)
}

// GetLastNReportsByID returns the last N reports for a given report ID
func (h *Handlers) GetLastNReportsByID(c *gin.Context) {
	// Get the id parameter from query string
	reportID := c.Query("id")
	if reportID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id' parameter"})
		return
	}

	// Get the reports from the database
	reports, err := h.db.GetLastNReportsByID(c.Request.Context(), reportID)
	if err != nil {
		log.Printf("Failed to get last N reports by ID %s: %v", reportID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetReportsByLatLng returns reports within a specified radius around given coordinates
func (h *Handlers) GetReportsByLatLng(c *gin.Context) {
	// Get the latitude parameter from query string
	latStr := c.Query("latitude")
	if latStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'latitude' parameter"})
		return
	}

	// Get the longitude parameter from query string
	lngStr := c.Query("longitude")
	if lngStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'longitude' parameter"})
		return
	}

	// Get the radius_km parameter from query string, default to 10 if not provided
	radiusKmStr := c.DefaultQuery("radius_km", "1.0")

	// Get the n parameter from query string, default to 10 if not provided
	nStr := c.DefaultQuery("n", "10")

	n := 10 // default value
	if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
		n = parsedN
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Parse latitude
	latitude, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'latitude' parameter. Must be a valid number."})
		return
	}

	// Parse longitude
	longitude, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'longitude' parameter. Must be a valid number."})
		return
	}

	// Parse radius_km
	radiusKm := 1.0 // default value
	if parsedRadius, err := strconv.ParseFloat(radiusKmStr, 32); err == nil && parsedRadius > 0 {
		radiusKm = parsedRadius
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'radius_km' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum radius to prevent abuse (e.g., 100 km)
	if radiusKm > 100 {
		radiusKm = 100
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByLatLng(c.Request.Context(), latitude, longitude, radiusKm, n)
	if err != nil {
		log.Printf("Failed to get reports by lat/lng (%.6f, %.6f, radius: %fkm): %v", latitude, longitude, radiusKm, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetReportsByLatLng returns reports within a specified radius around given coordinates
func (h *Handlers) GetReportsByLatLngLite(c *gin.Context) {
	// Get the latitude parameter from query string
	latStr := c.Query("latitude")
	if latStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'latitude' parameter"})
		return
	}

	// Get the longitude parameter from query string
	lngStr := c.Query("longitude")
	if lngStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'longitude' parameter"})
		return
	}

	// Get the radius_km parameter from query string, default to 10 if not provided
	radiusKmStr := c.DefaultQuery("radius_km", "1.0")

	// Get the n parameter from query string, default to 10 if not provided
	nStr := c.DefaultQuery("n", "10")

	n := 10 // default value
	if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
		n = parsedN
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Parse latitude
	latitude, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'latitude' parameter. Must be a valid number."})
		return
	}

	// Parse longitude
	longitude, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'longitude' parameter. Must be a valid number."})
		return
	}

	// Parse radius_km
	radiusKm := 1.0 // default value
	if parsedRadius, err := strconv.ParseFloat(radiusKmStr, 32); err == nil && parsedRadius > 0 {
		radiusKm = parsedRadius
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'radius_km' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum radius to prevent abuse (e.g., 100 km)
	if radiusKm > 100 {
		radiusKm = 100
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByLatLngLite(c.Request.Context(), latitude, longitude, radiusKm, n)
	if err != nil {
		log.Printf("Failed to get reports by lat/lng (%.6f, %.6f, radius: %fkm): %v", latitude, longitude, radiusKm, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handlers) GetReportsByBrand(c *gin.Context) {
	// Get the brand name parameter from query string
	brandName := c.Query("brand_name")
	if brandName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'brand_name' parameter"})
		return
	}

	// Get the limit parameter from query string, default to 10 if not provided
	limitStr := c.DefaultQuery("n", "10")

	limit := 10 // default value
	if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
		limit = parsedLimit
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'n' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum number of reports to prevent abuse
	if limit > MaxReportsLimit {
		limit = MaxReportsLimit
	}

	// Get the reports from the database
	reports, err := h.db.GetReportsByBrandName(c.Request.Context(), brandName, limit)
	if err != nil {
		log.Printf("Failed to get reports by brand '%s': %v", brandName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as other endpoints
	response := models.ReportBatch{
		Reports: reports,
		Count:   len(reports),
		FromSeq: 0,
		ToSeq:   0,
	}

	// Set FromSeq and ToSeq if there are reports
	if len(reports) > 0 {
		response.FromSeq = reports[0].Report.Seq
		response.ToSeq = reports[len(reports)-1].Report.Seq
	}

	c.JSON(http.StatusOK, response)
}

// GetImageBySeq returns a base64 encoded image for a specific report by sequence number
func (h *Handlers) GetImageBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing seq parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seq parameter. Must be a positive integer."})
		return
	}

	// Get the image from the database
	imageData, err := h.db.GetImageBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get image for report seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	// Encode image as base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Return the base64 encoded image
	c.JSON(http.StatusOK, gin.H{
		"image": base64Image,
	})
}

// GetRawImageBySeq returns the raw image data for a specific report by sequence number
func (h *Handlers) GetRawImageBySeq(c *gin.Context) {
	// Get the seq parameter from query string
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing seq parameter"})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seq parameter. Must be a positive integer."})
		return
	}

	// Get the image from the database
	imageData, err := h.db.GetImageBySeq(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get image for report seq %d: %v", seq, err)
		if err.Error() == fmt.Sprintf("report with seq %d not found", seq) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	// Detect image type from the data
	contentType := "image/jpeg" // default
	if len(imageData) > 4 {
		// Check for common image format signatures
		if imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47 {
			contentType = "image/png"
		} else if imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46 {
			contentType = "image/gif"
		} else if imageData[0] == 0x42 && imageData[1] == 0x4D {
			contentType = "image/bmp"
		} else if imageData[0] == 0xFF && imageData[1] == 0xD8 {
			contentType = "image/jpeg"
		}
	}

	// Set the appropriate Content-Type header for the image
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(imageData)))

	// Return the raw image data
	c.Data(http.StatusOK, contentType, imageData)
}
