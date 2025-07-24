package handlers

import (
	"log"
	"net/http"
	"strconv"

	"report_processor/database"
	"report_processor/models"

	"github.com/gin-gonic/gin"
)

// Handlers holds all HTTP handlers
type Handlers struct {
	db *database.Database
}

// NewHandlers creates a new handlers instance
func NewHandlers(db *database.Database) *Handlers {
	return &Handlers{db: db}
}

// MarkResolved marks a report as resolved
func (h *Handlers) MarkResolved(c *gin.Context) {
	var req models.MarkResolvedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}

	// Validate seq is positive
	if req.Seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Report sequence must be a positive integer",
		})
		return
	}

	// Mark the report as resolved
	err := h.db.MarkReportResolved(c.Request.Context(), req.Seq)
	if err != nil {
		log.Printf("Failed to mark report %d as resolved: %v", req.Seq, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to mark report as resolved",
			"error":   err.Error(),
		})
		return
	}

	response := models.MarkResolvedResponse{
		Success: true,
		Message: "Report marked as resolved successfully",
		Seq:     req.Seq,
		Status:  "resolved",
	}

	c.JSON(http.StatusOK, response)
}

// GetReportStatus gets the status of a specific report
func (h *Handlers) GetReportStatus(c *gin.Context) {
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Report sequence is required",
		})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid report sequence",
		})
		return
	}

	status, err := h.db.GetReportStatus(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get report status for seq %d: %v", seq, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get report status",
			"error":   err.Error(),
		})
		return
	}

	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Report status not found",
			"seq":     seq,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    status,
	})
}

// GetReportStatusCount gets the count of reports by status
func (h *Handlers) GetReportStatusCount(c *gin.Context) {
	counts, err := h.db.GetReportStatusCount(c.Request.Context())
	if err != nil {
		log.Printf("Failed to get report status count: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get report status count",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    counts,
	})
}
