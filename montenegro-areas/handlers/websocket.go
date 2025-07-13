package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"montenegro-areas/middleware"
	"montenegro-areas/models"
	ws "montenegro-areas/websocket"

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
		// Allow all origins for now
		// In production, you should implement proper origin checking
		return true
	},
}

// ListenMontenegroReports handles WebSocket connections for listening to reports in Montenegro
func (h *WebSocketHandler) ListenMontenegroReports(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r)
	log.Printf("INFO: WebSocket connection request from user %s", userID)

	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
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

	log.Printf("WebSocket connection established for Montenegro reports for user %s", userID)
}

// HealthCheck returns the service health status with WebSocket statistics
func (h *WebSocketHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r)
	log.Printf("INFO: WebSocket health check request from user %s", userID)

	connectedClients, lastBroadcastSeq := h.hub.GetStats()

	response := models.HealthResponse{
		Status:           "healthy",
		Service:          "montenegro-areas-websocket",
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		ConnectedClients: connectedClients,
		LastBroadcastSeq: lastBroadcastSeq,
	}

	// Set content type and write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Encode response as JSON
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode health response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
