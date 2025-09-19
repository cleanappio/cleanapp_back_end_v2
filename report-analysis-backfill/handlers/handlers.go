package handlers

import (
	"net/http"

	"report-analysis-backfill/service"

	"github.com/gin-gonic/gin"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	backfillService *service.BackfillService
}

// NewHandlers creates a new handlers instance
func NewHandlers(backfillService *service.BackfillService) *Handlers {
	return &Handlers{
		backfillService: backfillService,
	}
}

// HealthCheck returns the health status of the service
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "report-analysis-backfill",
	})
}

// GetStatus returns the current status of the backfill service
func (h *Handlers) GetStatus(c *gin.Context) {
	stats := h.backfillService.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"data":   stats,
	})
}
