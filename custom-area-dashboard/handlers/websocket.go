package handlers

import (
	"log"
	"net/http"
	"time"

	"custom-area-dashboard/middleware"
	"custom-area-dashboard/models"
	ws "custom-area-dashboard/websocket"

	"github.com/gin-gonic/gin"

	gorilla "github.com/gorilla/websocket"
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub *ws.Hub
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *ws.Hub) *WebSocketHandler {
	return &WebSocketHandler{
		hub: hub,
	}
}

// WebSocket upgrader
var upgrader = gorilla.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ListenCustomReports handles WebSocket connections for listening to reports in custom area
func (h *WebSocketHandler) ListenCustomReports(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket connection request from user %s", userID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection to WebSocket: %v", err)
		return
	}

	client := ws.NewClient(h.hub, conn)
	h.hub.Register <- client
	go client.WritePump()
	go client.ReadPump()

	log.Printf("WebSocket connection established for custom area reports for user %s", userID)
}

// HealthCheck returns the service health status with WebSocket statistics
func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket health check request from user %s", userID)

	connectedClients, lastBroadcastSeq := h.hub.GetStats()

	response := models.HealthResponse{
		Status:           "healthy",
		Service:          "custom-area-dashboard-websocket",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ConnectedClients: connectedClients,
		LastBroadcastSeq: lastBroadcastSeq,
	}

	c.JSON(200, response)
}
