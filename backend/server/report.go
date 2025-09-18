package server

import (
	"bytes"
	"cleanapp/common"
	"cleanapp/common/disburse"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"
	"cleanapp/backend/stxn"

	"github.com/apex/log"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// getReportAnalysisURL returns the report analysis URL from environment variable
func getReportAnalysisURL() string {
	return os.Getenv("REPORT_ANALYSIS_URL")
}

func Report(c *gin.Context) {
	var report = &api.ReportArgs{}

	// Get the arguments.
	if err := c.BindJSON(report); err != nil {
		log.Errorf("Failed to get the argument in /report call: %w", err)
		return
	}

	if report.Version != "2.0" {
		log.Errorf("Bad version in /report, expected: 2.0, got: %v", report.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("Error connecting to DB: %w", err)
		return
	}
	defer dbc.Close()

	// Add report to the database.
	savedReport, err := db.SaveReport(dbc, report)
	if err != nil {
		log.Errorf("Failed to write report with %w", err)
		c.String(http.StatusInternalServerError, "Failed to save the report.") // 500
		return
	}

	// Send report to analysis service if URL is configured
	analysisURL := getReportAnalysisURL()
	if analysisURL != "" {
		sendReportToAnalysis(savedReport, analysisURL)
	}

	c.JSON(http.StatusOK, api.ReportResponse{Seq: savedReport.Seq})

	go stxn.SendReport(ethcommon.HexToAddress(report.Id), disburse.ToWei(1.0))
}

// sendReportToAnalysis sends a report to the analysis service
func sendReportToAnalysis(report *api.Report, analysisURL string) {
	// Create the report data structure for the analysis service
	analysisReport := struct {
		Seq         int     `json:"seq"`
		Timestamp   string  `json:"timestamp"`
		ID          string  `json:"id"`
		Team        int     `json:"team"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		X           float64 `json:"x"`
		Y           float64 `json:"y"`
		Image       []byte  `json:"image"`
		ActionID    string  `json:"action_id"`
		Description string  `json:"description"`
	}{
		Seq:         report.Seq,
		Timestamp:   report.Timestamp,
		ID:          report.ID,
		Team:        report.Team,
		Latitude:    report.Latitude,
		Longitude:   report.Longitude,
		X:           report.X,
		Y:           report.Y,
		Image:       report.Image,
		ActionID:    report.ActionID,
		Description: report.Description,
	}

	// Marshal the report to JSON
	jsonData, err := json.Marshal(analysisReport)
	if err != nil {
		log.Errorf("Failed to marshal report for analysis: %w", err)
		return
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create the request
	req, err := http.NewRequest("POST", analysisURL+"/api/v3/analysis", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Errorf("Failed to create request for analysis service: %w", err)
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Failed to send report to analysis service: %w", err)
		return
	}
	defer resp.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read response from analysis service: %w", err)
		return
	}

	// Log the response
	log.Infof("Analysis service response for report %d (status %d): %s", report.Seq, resp.StatusCode)

	// Check response status
	if resp.StatusCode != http.StatusOK {
		log.Errorf("Analysis service returned non-OK status: %d, response: %s", resp.StatusCode, string(responseBody))
		return
	}

	log.Infof("Successfully sent report %d to analysis service", report.Seq)
}
