package handlers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"report-analyze-pipeline/database"
	"report-analyze-pipeline/rabbitmq"
	"report-analyze-pipeline/service"
	"github.com/gin-gonic/gin"
)

// Handlers represents the HTTP handlers
type Handlers struct {
	db *database.Database
	analysisService *service.Service
	subscriber *rabbitmq.Subscriber
}

// NewHandlers creates new HTTP handlers
func NewHandlers(db *database.Database, analysisService *service.Service, subscriber *rabbitmq.Subscriber) *Handlers {
	return &Handlers{db: db, analysisService: analysisService, subscriber: subscriber}
}

// HealthCheck handles health check requests
func (h *Handlers) HealthCheck(c *gin.Context) {
	var rmqConnected bool
	var rmqQueue, rmqExchange, rmqLastError string
	var rmqLastConnectAt, rmqLastDeliveryAt string

	if h.subscriber != nil {
		rmqConnected = h.subscriber.IsConnected()
		rmqQueue = h.subscriber.GetQueue()
		rmqExchange = h.subscriber.GetExchange()
		rmqLastError = h.subscriber.LastError()
		if t := h.subscriber.LastConnectAt(); !t.IsZero() {
			rmqLastConnectAt = t.UTC().Format(time.RFC3339)
		}
		if t := h.subscriber.LastDeliveryAt(); !t.IsZero() {
			rmqLastDeliveryAt = t.UTC().Format(time.RFC3339)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "report-analyze-pipeline",
		"rabbitmq_connected": rmqConnected,
		"rabbitmq_queue": rmqQueue,
		"rabbitmq_exchange": rmqExchange,
		"rabbitmq_last_connect_at": rmqLastConnectAt,
		"rabbitmq_last_delivery_at": rmqLastDeliveryAt,
		"rabbitmq_last_error": rmqLastError,
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
	SELECT seq, source, analysis_text, analysis_image, title, description, brand_name, brand_display_name, 
	       litter_probability, hazard_probability, digital_bug_probability, severity_level, summary, 
	       language, is_valid, classification, inferred_contact_emails, created_at
	FROM report_analysis
	WHERE seq = ?
	ORDER BY created_at DESC
	LIMIT 1`

	var analysis struct {
		Seq                   int     `json:"seq"`
		Source                string  `json:"source"`
		AnalysisText          string  `json:"analysis_text"`
		AnalysisImage         []byte  `json:"analysis_image,omitempty"`
		Title                 string  `json:"title"`
		Description           string  `json:"description"`
		BrandName             string  `json:"brand_name"`
		BrandDisplayName      string  `json:"brand_display_name"`
		LitterProbability     float64 `json:"litter_probability"`
		HazardProbability     float64 `json:"hazard_probability"`
		DigitalBugProbability float64 `json:"digital_bug_probability"`
		SeverityLevel         float64 `json:"severity_level"`
		Summary               string  `json:"summary"`
		Language              string  `json:"language"`
		IsValid               bool    `json:"is_valid"`
		Classification        string  `json:"classification"`
		InferredContactEmails string  `json:"inferred_contact_emails"`
		CreatedAt             string  `json:"created_at"`
	}

	err = h.db.GetDB().QueryRow(query, seq).Scan(
		&analysis.Seq,
		&analysis.Source,
		&analysis.AnalysisText,
		&analysis.AnalysisImage,
		&analysis.Title,
		&analysis.Description,
		&analysis.BrandName,
		&analysis.BrandDisplayName,
		&analysis.LitterProbability,
		&analysis.HazardProbability,
		&analysis.DigitalBugProbability,
		&analysis.SeverityLevel,
		&analysis.Summary,
		&analysis.Language,
		&analysis.IsValid,
		&analysis.Classification,
		&analysis.InferredContactEmails,
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

// DoAnalysis handles POST requests to perform analysis on a report
func (h *Handlers) DoAnalysis(c *gin.Context) {
	var report database.Report

	if err := c.ShouldBindJSON(&report); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid JSON format",
			"details": err.Error(),
		})
		return
	}

	log.Println("Received report for analysis:", report.Seq)

	// Analyze the report
	go func() {
		if err := h.analysisService.AnalyzeReport(&report); err != nil {
			log.Printf("Failed to analyze report %d: %v", report.Seq, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"message": "Analysis request received successfully",
		"report":  report,
	})
}
