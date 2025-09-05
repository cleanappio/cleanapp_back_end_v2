package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

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

				// If the report is resolved, update the report_status table and create response
				if resolved {
					err := h.db.MarkReportResolved(c.Request.Context(), r.Seq)
					if err != nil {
						log.Printf("Failed to mark report %d as resolved: %v", r.Seq, err)
						// Continue processing other reports even if one fails
					} else {
						log.Printf("Successfully marked report %d as resolved", r.Seq)

						// Create a verified response from the match request data
						_, err := h.db.CreateResponseFromMatchRequest(c.Request.Context(), req, r.Seq, "verified")
						if err != nil {
							log.Printf("Warning: failed to create verified response from match request: %v", err)
							// Continue processing other reports even if response creation fails
						} else {
							log.Printf("Successfully created verified response from match request")
						}
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
	highSimilarityCount := 0
	for _, result := range results {
		if result.Resolved {
			resolvedCount++
		}
		if result.Similarity > 0.7 {
			highSimilarityCount++
		}
	}

	// Check if we have high similarity reports but no resolved ones
	if highSimilarityCount > 0 && resolvedCount == 0 {
		log.Printf("Found %d high similarity reports (>0.7) but none resolved. Submitting as new report.", highSimilarityCount)

		// Submit the original request as a new report
		err := h.submitReport(c.Request.Context(), req)
		if err != nil {
			log.Printf("Failed to submit report: %v", err)
			// Continue with response even if submission fails
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

// submitReport submits a report to the reports submission service
func (h *Handlers) submitReport(ctx context.Context, req models.MatchReportRequest) error {
	if h.config.ReportsSubmissionURL == "" {
		log.Printf("Reports submission URL not configured, skipping submission")
		return nil
	}

	// Prepare the report submission payload
	reportPayload := map[string]interface{}{
		"version":    req.Version,
		"id":         req.ID,
		"latitude":   req.Latitude,
		"longitude":  req.Longitude,
		"x":          req.X,
		"y":          req.Y,
		"image":      req.Image,
		"action_id":  "",
		"annotation": "",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(reportPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal report payload: %w", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Submit the report
	url := h.config.ReportsSubmissionURL + "/report"
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to submit report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("report submission failed with status %d", resp.StatusCode)
	}

	log.Printf("Successfully submitted report %s to %s", req.ID, url)
	return nil
}

// GetResponse gets a specific response by seq
func (h *Handlers) GetResponse(c *gin.Context) {
	seqStr := c.Query("seq")
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Response sequence is required",
		})
		return
	}

	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid response sequence",
		})
		return
	}

	response, err := h.db.GetResponse(c.Request.Context(), seq)
	if err != nil {
		log.Printf("Failed to get response for seq %d: %v", seq, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get response",
			"error":   err.Error(),
		})
		return
	}

	if response == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Response not found",
			"seq":     seq,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// GetResponsesByStatus gets responses by status
func (h *Handlers) GetResponsesByStatus(c *gin.Context) {
	status := c.Query("status")
	if status == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Status parameter is required",
		})
		return
	}

	if status != "resolved" && status != "verified" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Status must be either 'resolved' or 'verified'",
		})
		return
	}

	responses, err := h.db.GetResponsesByStatus(c.Request.Context(), status)
	if err != nil {
		log.Printf("Failed to get responses by status %s: %v", status, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to get responses by status",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    responses,
		"count":   len(responses),
	})
}
