package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"cleanapp-common/httpx"
	"report_processor/config"
	"report_processor/database"
	"report_processor/models"
	"report_processor/openai"
	"report_processor/rabbitmq"
	"report_processor/services"

	"github.com/gin-gonic/gin"
)

// Handlers holds all HTTP handlers
type Handlers struct {
	db                *database.Database
	config            *config.Config
	openaiClient      *openai.Client
	tagClient         *services.TagClient
	rabbitmqPublisher *rabbitmq.Publisher
}

type wireSubmitResponse struct {
	ReceiptID         string `json:"receipt_id"`
	SubmissionID      string `json:"submission_id"`
	SourceID          string `json:"source_id"`
	Status            string `json:"status"`
	Lane              string `json:"lane"`
	ReportID          int    `json:"report_id,omitempty"`
	IdempotencyReplay bool   `json:"idempotency_replay"`
}

type wireStatusResponse struct {
	SourceID          string `json:"source_id"`
	ReceiptID         string `json:"receipt_id"`
	SubmissionID      string `json:"submission_id"`
	Status            string `json:"status"`
	Lane              string `json:"lane"`
	ReportID          int    `json:"report_id,omitempty"`
	IdempotencyReplay bool   `json:"idempotency_replay"`
}

// NewHandlers creates a new handlers instance
func NewHandlers(db *database.Database, cfg *config.Config, rabbitmqPublisher *rabbitmq.Publisher) *Handlers {
	var openaiClient *openai.Client
	if cfg.OpenAIAPIKey != "" {
		openaiClient = openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	}

	tagClient := services.NewTagClient(cfg.TagServiceURL)

	return &Handlers{
		db:                db,
		config:            cfg,
		openaiClient:      openaiClient,
		tagClient:         tagClient,
		rabbitmqPublisher: rabbitmqPublisher,
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
	err := h.db.MarkReportResolved(context.Background(), req.Seq)
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

	status, err := h.db.GetReportStatus(context.Background(), seq)
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
	counts, err := h.db.GetReportStatusCount(context.Background())
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
	reports, err := h.db.GetReportsInRadius(context.Background(), req.Latitude, req.Longitude, h.config.ReportsRadiusMeters)
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
				similarity, resolved := h.compareImages(req.Image, r.Image, r.AnalysisText, req.Latitude, req.Longitude, r.Latitude, r.Longitude)

				// If the report is resolved, update the report_status table and create response
				if resolved {
					err := h.db.MarkReportResolved(context.Background(), r.Seq)
					if err != nil {
						log.Printf("Failed to mark report %d as resolved: %v", r.Seq, err)
						// Continue processing other reports even if one fails
					} else {
						log.Printf("Successfully marked report %d as resolved", r.Seq)

						// Create a verified response from the match request data
						_, err := h.db.CreateResponseFromMatchRequest(context.Background(), req, r.Seq, "verified")
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
	for _, result := range results {
		if result.Resolved {
			resolvedCount++
		}
	}

	// Check if we have high similarity reports but no resolved ones
	if resolvedCount == 0 {
		log.Println("None of existing reports resolved. Submitting as new report.")

		// Find the highest similarity report that's not resolved (this will be our primary_seq)
		primarySeq := -1
		var maxSimilarity float64
		for _, result := range results {
			if !result.Resolved && result.Similarity > maxSimilarity {
				maxSimilarity = result.Similarity
				primarySeq = result.ReportSeq
			}
		}

		// Submit the original request as a new report
		newReportSeq, err := h.submitReport(context.Background(), req)
		if err != nil {
			log.Printf("Failed to submit report: %v", err)
			// Continue with response even if submission fails
		} else if newReportSeq > 0 && primarySeq > 0 && maxSimilarity >= 0.7 {
			// Create the cluster relationship with the returned sequence number
			err = h.db.InsertReportCluster(context.Background(), primarySeq, newReportSeq)
			if err != nil {
				log.Printf("Failed to create report cluster: %v", err)
			} else {
				log.Printf("Created report cluster: primary_seq=%d, related_seq=%d", primarySeq, newReportSeq)
			}
		} else {
			log.Println("No report cluster created. Submitting as new report.")
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
func (h *Handlers) compareImages(image1, image2 []byte, originalDescription string, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng float64) (float64, bool) {
	// If OpenAI client is not available, return default values
	if h.openaiClient == nil {
		log.Printf("OpenAI client not available, returning default comparison values")
		return 0.0, false
	}

	// Use OpenAI API to compare images
	similarity, litterRemoved, err := h.openaiClient.CompareImages(image1, image2, originalDescription, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng)
	if err != nil {
		log.Printf("Failed to compare images with OpenAI: %v", err)
		return 0.0, false
	}

	// Consider it a match if similarity is above 0.7 and litter was removed
	resolved := similarity >= 0.7 && litterRemoved

	return similarity, resolved
}

// submitReport submits a report to the reports submission service and returns the new report's sequence number
func (h *Handlers) submitReport(ctx context.Context, req models.MatchReportRequest) (int, error) {
	switch h.reportsSubmissionProtocol() {
	case "wire":
		return h.submitReportViaWire(ctx, req)
	default:
		return h.submitReportLegacy(ctx, req)
	}
}

func (h *Handlers) submitReportLegacy(ctx context.Context, req models.MatchReportRequest) (int, error) {
	if strings.TrimSpace(h.config.ReportsSubmissionURL) == "" {
		log.Printf("Reports submission URL not configured, skipping submission")
		return 0, nil
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
		"annotation": req.Annotation,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(reportPayload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal report payload: %w", err)
	}

	// Create HTTP client with timeout
	client := httpx.NewClient(30 * time.Second)

	// Submit the report
	url := h.config.ReportsSubmissionURL + "/report"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, fmt.Errorf("failed to build report submission request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("failed to submit report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("report submission failed with status %d", resp.StatusCode)
	}

	// Parse response to get the sequence number
	var response struct {
		Seq int `json:"seq"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Printf("Failed to parse response body, but report was submitted successfully: %v", err)
		// Fallback: get the latest sequence number from the database
		latestSeq, dbErr := h.db.GetLatestReportSeq(ctx)
		if dbErr != nil {
			log.Printf("Failed to get latest report seq from database: %v", dbErr)
			return 0, nil // Report was submitted but we couldn't get the seq
		}
		log.Printf("Successfully submitted report %s to %s, using latest seq %d from database", req.ID, url, latestSeq)
		return latestSeq, nil
	}

	log.Printf("Successfully submitted report %s to %s with seq %d", req.ID, url, response.Seq)

	// Always publish report.raw so downstream consumers (analysis, tags, etc.) can react.
	// Tags are optional; consumers that need tags can ignore messages without them.
	if h.rabbitmqPublisher != nil {
		rawEvent := struct {
			Seq  int      `json:"seq"`
			Tags []string `json:"tags,omitempty"`
		}{
			Seq:  response.Seq,
			Tags: req.Tags,
		}

		err := h.rabbitmqPublisher.PublishWithRoutingKey(h.config.RabbitMQRawReportRoutingKey, rawEvent)
		if err != nil {
			// Don't fail the whole operation for async processing
			log.Printf("Failed to publish report.raw event for report %d: %v", response.Seq, err)
		} else if len(req.Tags) > 0 {
			log.Printf("Published report.raw event for report %d with %d tags", response.Seq, len(req.Tags))
		} else {
			log.Printf("Published report.raw event for report %d (no tags)", response.Seq)
		}
	} else {
		log.Printf("RabbitMQ publisher not available, skipping report.raw event for report %d", response.Seq)
	}

	return response.Seq, nil
}

func (h *Handlers) submitReportViaWire(ctx context.Context, req models.MatchReportRequest) (int, error) {
	wireBaseURL := strings.TrimSpace(h.config.ReportsSubmissionWireURL)
	if wireBaseURL == "" {
		if h.config.ReportsSubmissionProtocol == "wire" {
			return 0, fmt.Errorf("wire submission requested but REPORTS_SUBMISSION_WIRE_URL is not configured")
		}
		return h.submitReportLegacy(ctx, req)
	}
	if strings.TrimSpace(h.config.ReportsSubmissionToken) == "" {
		if h.config.ReportsSubmissionProtocol == "wire" {
			return 0, fmt.Errorf("wire submission requested but REPORTS_SUBMISSION_TOKEN is not configured")
		}
		return h.submitReportLegacy(ctx, req)
	}

	body, sourceID, err := h.buildWireSubmissionPayload(req)
	if err != nil {
		return 0, err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal wire payload: %w", err)
	}

	client := httpx.NewClient(30 * time.Second)
	submitURL := strings.TrimRight(wireBaseURL, "/") + "/api/v1/agent-reports:submit"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewBuffer(payload))
	if err != nil {
		return 0, fmt.Errorf("failed to build wire request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.config.ReportsSubmissionToken)

	resp, err := client.Do(httpReq)
	if err != nil {
		if h.config.ReportsSubmissionProtocol == "auto" {
			log.Printf("Wire submission failed for report %s, falling back to legacy submit: %v", req.ID, err)
			return h.submitReportLegacy(ctx, req)
		}
		return 0, fmt.Errorf("failed to submit report via wire: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if h.config.ReportsSubmissionProtocol == "auto" {
			log.Printf("Wire submission returned status %d for report %s, falling back to legacy submit", resp.StatusCode, req.ID)
			return h.submitReportLegacy(ctx, req)
		}
		return 0, fmt.Errorf("wire submission failed with status %d", resp.StatusCode)
	}

	var wireResp wireSubmitResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return 0, fmt.Errorf("failed to decode wire response: %w", err)
	}
	if wireResp.ReportID > 0 {
		log.Printf("Successfully submitted report %s via Wire with seq %d (lane=%s receipt=%s)", req.ID, wireResp.ReportID, wireResp.Lane, wireResp.ReceiptID)
		return wireResp.ReportID, nil
	}

	statusResp, err := h.lookupWireStatusBySourceID(ctx, client, wireBaseURL, sourceID)
	if err != nil {
		return 0, fmt.Errorf("wire submission accepted but status lookup failed: %w", err)
	}
	if statusResp.ReportID <= 0 {
		return 0, fmt.Errorf("wire submission accepted but no report id was assigned yet for source_id %s", sourceID)
	}
	log.Printf("Successfully submitted report %s via Wire with seq %d after status lookup (lane=%s receipt=%s)", req.ID, statusResp.ReportID, statusResp.Lane, statusResp.ReceiptID)
	return statusResp.ReportID, nil
}

func (h *Handlers) buildWireSubmissionPayload(req models.MatchReportRequest) (map[string]any, string, error) {
	sourceID := strings.TrimSpace(req.ID)
	if sourceID == "" {
		return nil, "", fmt.Errorf("report id is required for wire submission")
	}
	submittedAt := time.Now().UTC().Format(time.RFC3339)
	inlineImage := base64.StdEncoding.EncodeToString(req.Image)
	imageHash := sha256.Sum256(req.Image)
	title := strings.TrimSpace(req.Annotation)
	if title == "" {
		title = "Matched report submission"
	}
	if len(title) > 255 {
		title = title[:255]
	}
	description := strings.TrimSpace(req.Annotation)
	if description == "" {
		description = fmt.Sprintf("Matched report submission %s", sourceID)
	}

	body := map[string]any{
		"schema_version": "cleanapp-wire.v1",
		"source_id":      sourceID,
		"submitted_at":   submittedAt,
		"observed_at":    submittedAt,
		"agent": map[string]any{
			"agent_id":         "report-processor",
			"agent_name":       "report-processor",
			"agent_type":       "internal_service",
			"operator_type":    "internal",
			"auth_method":      "api_key",
			"software_version": "report-processor",
		},
		"provenance": map[string]any{
			"generation_method": "report_processor_match",
			"chain_of_custody":  []string{"report_processor_match", "wire_submit"},
			"human_in_loop":     false,
		},
		"report": map[string]any{
			"domain":       "physical",
			"problem_type": "matched_report_submission",
			"title":        title,
			"description":  description,
			"language":     "en",
			"confidence":   0.95,
			"location": map[string]any{
				"kind":             "coordinate",
				"lat":              req.Latitude,
				"lng":              req.Longitude,
				"place_confidence": 1.0,
			},
			"target_entity": map[string]any{
				"target_type": "organization",
				"name":        "report-processor",
			},
			"evidence_bundle": []map[string]any{
				{
					"evidence_id": "inline-image",
					"type":        "inline_image",
					"sha256":      hex.EncodeToString(imageHash[:]),
					"mime_type":   "application/octet-stream",
					"captured_at": submittedAt,
				},
			},
			"tags": req.Tags,
		},
		"extensions": map[string]any{
			"image_base64": inlineImage,
			"source_type":  "vision",
			"x":            req.X,
			"y":            req.Y,
		},
	}
	return body, sourceID, nil
}

func (h *Handlers) lookupWireStatusBySourceID(ctx context.Context, client *http.Client, wireBaseURL, sourceID string) (*wireStatusResponse, error) {
	statusURL := strings.TrimRight(wireBaseURL, "/") + "/api/v1/agent-reports/status/" + url.PathEscape(sourceID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build wire status request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+h.config.ReportsSubmissionToken)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup wire status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wire status lookup failed with status %d", resp.StatusCode)
	}

	var statusResp wireStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode wire status response: %w", err)
	}
	return &statusResp, nil
}

func (h *Handlers) reportsSubmissionProtocol() string {
	mode := strings.ToLower(strings.TrimSpace(h.config.ReportsSubmissionProtocol))
	switch mode {
	case "wire":
		return "wire"
	case "auto":
		if strings.TrimSpace(h.config.ReportsSubmissionWireURL) != "" && strings.HasPrefix(strings.TrimSpace(h.config.ReportsSubmissionToken), "cleanapp_fk_") {
			return "wire"
		}
		return "legacy"
	default:
		return "legacy"
	}
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

	response, err := h.db.GetResponse(context.Background(), seq)
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

	responses, err := h.db.GetResponsesByStatus(context.Background(), status)
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
