package handlers

import (
	"log"
	"net/http"
	"time"

	"montenegro-areas/middleware"
	"montenegro-areas/models"
	ws "montenegro-areas/websocket"

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

// ListenMontenegroReports handles WebSocket connections for listening to reports in Montenegro
func (h *WebSocketHandler) ListenMontenegroReports(c *gin.Context) {
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

	log.Printf("WebSocket connection established for Montenegro reports for user %s", userID)
}

// HealthCheck returns the service health status with WebSocket statistics
func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket health check request from user %s", userID)

	connectedClients, lastBroadcastSeq := h.hub.GetStats()

	response := models.HealthResponse{
		Status:           "healthy",
		Service:          "montenegro-areas-websocket",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ConnectedClients: connectedClients,
		LastBroadcastSeq: lastBroadcastSeq,
	}

	c.JSON(200, response)
}
