package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"report-listener/database"
	"report-listener/models"
	ws "report-listener/websocket"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
)

const (
	// MaxReportsLimit is the maximum number of reports that can be requested in a single query
	MaxReportsLimit = 1000
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
	reports, err := h.db.GetLastNAnalyzedReports(c.Request.Context(), limit)
	if err != nil {
		log.Printf("Failed to get last N analyzed reports: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve reports"})
		return
	}

	// Create the response in the same format as WebSocket broadcasts
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

	// Get the N parameter from query string, default to 10 if not provided
	nStr := c.DefaultQuery("N", "10")

	n := 10 // default value
	if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
		n = parsedN
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'N' parameter. Must be a positive integer."})
		return
	}

	// Limit the maximum number of reports to prevent abuse
	if n > MaxReportsLimit {
		n = MaxReportsLimit
	}

	// Get the reports from the database
	reports, err := h.db.GetLastNReportsByID(c.Request.Context(), reportID, n)
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
