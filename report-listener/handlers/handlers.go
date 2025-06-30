package handlers

import (
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
	if limit > 100 {
		limit = 100
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
