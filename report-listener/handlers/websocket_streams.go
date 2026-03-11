package handlers

import (
	"log"
	"net/http"
	"strings"

	"cleanapp-common/authx"
	ws "report-listener/websocket"

	"github.com/gin-gonic/gin"
)

func (h *Handlers) listenOnHub(c *gin.Context, hub *ws.Hub, streamName string) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade %s websocket connection: %v", streamName, err)
		return
	}

	client := ws.NewClient(hub, conn)
	hub.Register <- client

	go client.WritePump()
	go client.ReadPump()

	log.Printf("%s websocket connection established", streamName)
}

func (h *Handlers) hasPrivilegedStreamAccess(c *gin.Context) bool {
	internalToken := strings.TrimSpace(c.GetHeader("X-Internal-Admin-Token"))
	if internalToken != "" && h.cfg.InternalAdminToken != "" && internalToken == h.cfg.InternalAdminToken {
		return true
	}

	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if token == "" {
		return false
	}

	if _, err := authx.VerifyAccessToken(c.Request.Context(), h.db.DB(), token, h.cfg.JWTSecret); err == nil {
		return true
	}

	return false
}

func (h *Handlers) ListenReports(c *gin.Context) {
	if h.hasPrivilegedStreamAccess(c) {
		h.listenOnHub(c, h.hub, "privileged/full")
		return
	}
	h.listenOnHub(c, h.publicHub, "public-lite")
}

func (h *Handlers) ListenPublicReports(c *gin.Context) {
	h.listenOnHub(c, h.publicHub, "public-lite")
}

func (h *Handlers) ListenPrivilegedReports(c *gin.Context) {
	if !h.hasPrivilegedStreamAccess(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "privileged websocket access required"})
		return
	}
	h.listenOnHub(c, h.hub, "privileged/full")
}
