package handlers

import (
	"log"

	"brand-dashboard/middleware"
	"brand-dashboard/models"
	"brand-dashboard/services"
	"cleanapp-common/edge"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub            *services.WebSocketHub
	allowedOrigins []string
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *services.WebSocketHub, allowedOrigins []string) *WebSocketHandler {
	return &WebSocketHandler{
		hub:            hub,
		allowedOrigins: allowedOrigins,
	}
}

// ListenBrandReports handles WebSocket connections for brand reports
func (h *WebSocketHandler) ListenBrandReports(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket connection request from user %s", userID)

	// Upgrade the HTTP connection to WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: edge.WebSocketOriginChecker(h.allowedOrigins),
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Error upgrading connection to WebSocket: %v", err)
		return
	}

	// Register the client with the hub
	h.hub.RegisterClient(conn, userID)
	log.Printf("INFO: WebSocket client registered for user %s", userID)
}

// HealthCheck handles WebSocket health check
func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
	response := models.HealthResponse{
		Status:           "healthy",
		Message:          "Brand Dashboard WebSocket service is running",
		Service:          "brand-dashboard-websocket",
		ConnectedClients: h.hub.GetConnectedClientsCount(),
		LastBroadcastSeq: h.hub.GetLastBroadcastSeq(),
	}
	c.JSON(200, response)
}
