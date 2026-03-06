package handlers

import (
	"custom-area-dashboard/config"
	"custom-area-dashboard/middleware"
	"custom-area-dashboard/models"
	ws "custom-area-dashboard/websocket"
	"log"
	"net/http"
	"time"

	"cleanapp-common/edge"
	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
)

type WebSocketHandler struct {
	hub            *ws.Hub
	allowedOrigins []string
}

func NewWebSocketHandler(hub *ws.Hub, cfg *config.Config) *WebSocketHandler {
	allowed := []string(nil)
	if cfg != nil {
		allowed = cfg.WebSocketAllowedOrigins
	}
	return &WebSocketHandler{hub: hub, allowedOrigins: allowed}
}

func (h *WebSocketHandler) ListenCustomReports(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket connection request from user %s", userID)
	upgrader := gorilla.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     edge.WebSocketOriginChecker(h.allowedOrigins),
	}
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

func (h *WebSocketHandler) HealthCheck(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: WebSocket health check request from user %s", userID)
	connectedClients, lastBroadcastSeq := h.hub.GetStats()
	response := models.HealthResponse{Status: "healthy", Service: "custom-area-dashboard-websocket", Timestamp: time.Now().UTC().Format(time.RFC3339), ConnectedClients: connectedClients, LastBroadcastSeq: lastBroadcastSeq}
	c.JSON(http.StatusOK, response)
}
