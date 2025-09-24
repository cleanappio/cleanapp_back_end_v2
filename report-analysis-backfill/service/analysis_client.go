package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"report-analysis-backfill/config"
	"report-analysis-backfill/database"
	"report-analysis-backfill/image"
	"report-analysis-backfill/models"
)

// AnalysisClient handles communication with the analysis API
type AnalysisClient struct {
	baseURL    string
	httpClient *http.Client
	db         *database.Database
}

// NewAnalysisClient creates a new analysis client
func NewAnalysisClient(cfg *config.Config, db *database.Database) *AnalysisClient {
	return &AnalysisClient{
		baseURL: cfg.ReportAnalysisURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		db: db,
	}
}

// SendReport sends a report to the analysis API
func (c *AnalysisClient) SendReport(report *models.Report) error {
	// Create a copy of the report to avoid modifying the original
	reportCopy := *report

	// Fetch the image for this report
	imageData, err := c.db.GetReportImage(report.Seq)
	if err != nil {
		log.Printf("Failed to fetch image for report seq=%d: %v", report.Seq, err)
		// Continue without image if fetch fails
		reportCopy.Image = nil
	} else {
		// Compress the image if it exists
		if len(imageData) > 0 {
			compressedImage, err := image.CompressImage(imageData)
			if err != nil {
				log.Printf("Failed to compress image for report seq=%d: %v", report.Seq, err)
				// Continue with original image if compression fails
				reportCopy.Image = imageData
			} else {
				reportCopy.Image = compressedImage
				log.Printf("Image compressed for report seq=%d: %d bytes -> %d bytes",
					report.Seq, len(imageData), len(compressedImage))
			}
		} else {
			reportCopy.Image = nil
		}
	}

	// Prepare the request body
	requestBody, err := json.Marshal(reportCopy)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	// Create the request
	url := fmt.Sprintf("%s/api/v3/analysis", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for successful response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("analysis API returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Successfully sent report seq=%d to analysis API", report.Seq)
	return nil
}

// SendReportsBatch sends a batch of reports to the analysis API
func (c *AnalysisClient) SendReportsBatch(reports []models.Report) error {
	successCount := 0
	errorCount := 0

	for _, report := range reports {
		if err := c.SendReport(&report); err != nil {
			log.Printf("Failed to send report seq=%d: %v", report.Seq, err)
			errorCount++
		} else {
			successCount++
		}
	}

	log.Printf("Batch processing complete: %d successful, %d failed", successCount, errorCount)
	return nil
}
