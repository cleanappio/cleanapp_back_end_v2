package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"report_processor/config"
	"report_processor/database"
	"report_processor/models"
	"report_processor/openai"

	"github.com/gin-gonic/gin"
)

// Handlers holds all HTTP handlers
type Handlers struct {
	db           *database.Database
	config       *config.Config
	openaiClient *openai.Client
}

// NewHandlers creates a new handlers instance
func NewHandlers(db *database.Database, cfg *config.Config) *Handlers {
	var openaiClient *openai.Client
	if cfg.OpenAIAPIKey != "" {
		openaiClient = openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	}

	return &Handlers{
		db:           db,
		config:       cfg,
		openaiClient: openaiClient,
	}
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

// MatchReport matches a report against reports in the database within a 10m radius
func (h *Handlers) MatchReport(c *gin.Context) {
	var req models.MatchReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}

	// Validate version
	if req.Version != "2.0" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Unsupported version. Expected '2.0'",
		})
		return
	}

	// Validate coordinates
	if req.Latitude < -90 || req.Latitude > 90 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid latitude. Must be between -90 and 90",
		})
		return
	}
	if req.Longitude < -180 || req.Longitude > 180 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid longitude. Must be between -180 and 180",
		})
		return
	}

	// Validate x, y coordinates
	if req.X < 0 || req.X > 1 || req.Y < 0 || req.Y > 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid x,y coordinates. Must be between 0 and 1",
		})
		return
	}

	// Get reports within configured radius
	reports, err := h.db.GetReportsInRadius(c.Request.Context(), req.Latitude, req.Longitude, h.config.ReportsRadiusMeters)
	if err != nil {
		log.Printf("Failed to get reports in radius: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get reports in radius",
			"error":   err.Error(),
		})
		return
	}

	// Compare the provided image with each report image in parallel chunks
	const chunkSize = 10
	var results []models.MatchResult

	log.Printf("Processing %d reports in chunks of %d", len(reports), chunkSize)

	// Process reports in chunks
	for i := 0; i < len(reports); i += chunkSize {
		end := i + chunkSize
		if end > len(reports) {
			end = len(reports)
		}

		chunk := reports[i:end]
		log.Printf("Processing chunk %d-%d (%d reports)", i+1, end, len(chunk))

		// Process current chunk in parallel
		var wg sync.WaitGroup
		var mu sync.Mutex
		var chunkResults []models.MatchResult

		for _, report := range chunk {
			wg.Add(1)
			go func(r models.Report) {
				defer wg.Done()

				log.Printf("Comparing report %d (%f, %f)", r.Seq, r.Latitude, r.Longitude)

				// Compare images
				similarity, resolved := h.compareImages(req.Image, r.Image, req.Latitude, req.Longitude, r.Latitude, r.Longitude)

				// If the report is resolved, update the report_status table
				if resolved {
					err := h.db.MarkReportResolved(c.Request.Context(), r.Seq)
					if err != nil {
						log.Printf("Failed to mark report %d as resolved: %v", r.Seq, err)
						// Continue processing other reports even if one fails
					} else {
						log.Printf("Successfully marked report %d as resolved", r.Seq)
					}
				}

				// Thread-safe append to chunk results
				mu.Lock()
				chunkResults = append(chunkResults, models.MatchResult{
					ReportSeq:  r.Seq,
					Similarity: similarity,
					Resolved:   resolved,
				})
				mu.Unlock()
			}(report)
		}

		// Wait for current chunk to complete
		wg.Wait()

		// Add chunk results to overall results
		results = append(results, chunkResults...)

		log.Printf("Completed chunk %d-%d", i+1, end)
	}

	// Count resolved reports for logging
	resolvedCount := 0
	for _, result := range results {
		if result.Resolved {
			resolvedCount++
		}
	}

	response := models.MatchReportResponse{
		Success: true,
		Message: fmt.Sprintf("Report matching completed. %d reports resolved out of %d compared.", resolvedCount, len(results)),
		Results: results,
	}

	c.JSON(http.StatusOK, response)
}

// compareImages compares two images and returns similarity score and resolved status
func (h *Handlers) compareImages(image1, image2 []byte, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng float64) (float64, bool) {
	// If OpenAI client is not available, return default values
	if h.openaiClient == nil {
		log.Printf("OpenAI client not available, returning default comparison values")
		return 0.0, false
	}

	// Use OpenAI API to compare images
	similarity, litterRemoved, err := h.openaiClient.CompareImages(image1, image2, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng)
	if err != nil {
		log.Printf("Failed to compare images with OpenAI: %v", err)
		return 0.0, false
	}

	// Consider it a match if similarity is above 0.7 and litter was removed
	resolved := similarity >= 0.7 && litterRemoved

	return similarity, resolved
}
