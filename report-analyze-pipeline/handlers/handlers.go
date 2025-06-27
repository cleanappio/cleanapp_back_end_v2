package handlers

import (
	"net/http"
	"strconv"

	"report-analyze-pipeline/database"

	"github.com/gin-gonic/gin"
)

// Handlers represents the HTTP handlers
type Handlers struct {
	db *database.Database
}

// NewHandlers creates new HTTP handlers
func NewHandlers(db *database.Database) *Handlers {
	return &Handlers{db: db}
}

// HealthCheck handles health check requests
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "report-analyze-pipeline",
	})
}

// GetAnalysisStatus returns the status of report analysis
func (h *Handlers) GetAnalysisStatus(c *gin.Context) {
	lastProcessedSeq, err := h.db.GetLastProcessedSeq()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get analysis status",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"last_processed_seq": lastProcessedSeq,
		"service":            "report-analyze-pipeline",
	})
}

// GetAnalysisBySeq returns analysis for a specific report sequence
func (h *Handlers) GetAnalysisBySeq(c *gin.Context) {
	seqStr := c.Param("seq")
	seq, err := strconv.Atoi(seqStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid sequence number",
		})
		return
	}

	// Query the database for analysis
	query := `
	SELECT seq, source, analysis_text, analysis_image, created_at
	FROM report_analysis
	WHERE seq = ?
	ORDER BY created_at DESC
	LIMIT 1`

	var analysis struct {
		Seq           int    `json:"seq"`
		Source        string `json:"source"`
		AnalysisText  string `json:"analysis_text"`
		AnalysisImage []byte `json:"analysis_image,omitempty"`
		CreatedAt     string `json:"created_at"`
	}

	err = h.db.GetDB().QueryRow(query, seq).Scan(
		&analysis.Seq,
		&analysis.Source,
		&analysis.AnalysisText,
		&analysis.AnalysisImage,
		&analysis.CreatedAt,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Analysis not found",
		})
		return
	}

	// Don't include image data in JSON response unless specifically requested
	if c.Query("include_image") != "true" {
		analysis.AnalysisImage = nil
	}

	c.JSON(http.StatusOK, analysis)
}

// GetAnalysisStats returns statistics about report analysis
func (h *Handlers) GetAnalysisStats(c *gin.Context) {
	// Get total analyzed reports
	var totalAnalyzed int
	err := h.db.GetDB().QueryRow("SELECT COUNT(*) FROM report_analysis").Scan(&totalAnalyzed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get analysis stats",
		})
		return
	}

	// Get total reports
	var totalReports int
	err = h.db.GetDB().QueryRow("SELECT COUNT(*) FROM reports").Scan(&totalReports)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get total reports count",
		})
		return
	}

	// Get analysis by source
	rows, err := h.db.GetDB().Query(`
		SELECT source, COUNT(*) as count
		FROM report_analysis
		GROUP BY source
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get analysis by source",
		})
		return
	}
	defer rows.Close()

	sourceStats := make(map[string]int)
	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			continue
		}
		sourceStats[source] = count
	}

	c.JSON(http.StatusOK, gin.H{
		"total_reports":      totalReports,
		"total_analyzed":     totalAnalyzed,
		"pending_analysis":   totalReports - totalAnalyzed,
		"analysis_by_source": sourceStats,
	})
}
