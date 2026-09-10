package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"report-listener/database"
	"report-listener/models"

	"github.com/gin-gonic/gin"
)

type reportSortSubmissionRequest struct {
	SorterID     string `json:"sorter_id"`
	ReportSeq    int    `json:"report_seq"`
	Verdict      string `json:"verdict"`
	UrgencyScore int    `json:"urgency_score"`
}

func (h *Handlers) GetNextSortReport(c *gin.Context) {
	sorterID := strings.TrimSpace(c.Query("sorter_id"))
	if sorterID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing sorter_id parameter"})
		return
	}

	candidate, err := h.db.GetNextSortableReport(c.Request.Context(), sorterID)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrNoSortableReports):
			c.JSON(http.StatusNotFound, gin.H{"error": "No sortable reports are available"})
		case errors.Is(err, database.ErrInvalidSortVote):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sorter_id"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load next sortable report"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"report": gin.H{
			"seq":       candidate.Report.Seq,
			"public_id": candidate.Report.PublicID,
			"timestamp": candidate.Report.Timestamp,
		},
		"sort_metrics": candidate.SortMetrics,
	})
}

func (h *Handlers) SubmitSortReport(c *gin.Context) {
	var req reportSortSubmissionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if req.ReportSeq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "report_seq must be a positive integer"})
		return
	}
	if req.UrgencyScore < 0 || req.UrgencyScore > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "urgency_score must be between 0 and 10"})
		return
	}

	result, err := h.db.SubmitReportSort(c.Request.Context(), models.ReportSortVote{
		SorterID:     req.SorterID,
		ReportSeq:    req.ReportSeq,
		Verdict:      req.Verdict,
		UrgencyScore: req.UrgencyScore,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		switch {
		case errors.Is(err, database.ErrDuplicateSortVote):
			c.JSON(http.StatusConflict, gin.H{"error": "Report already sorted by this user"})
		case errors.Is(err, database.ErrOwnReportSort):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot sort your own report"})
		case errors.Is(err, database.ErrSortTargetMissing):
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		case errors.Is(err, database.ErrInvalidSortVote):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sort submission"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit report sort"})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}
