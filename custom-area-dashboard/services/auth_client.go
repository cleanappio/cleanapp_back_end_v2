package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"custom-area-dashboard/models"
)

// AuthClient handles communication with the auth service
type ReportAuthClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAuthClient creates a new auth client
func NewReportAuthClient(authServiceURL string) *ReportAuthClient {
	return &ReportAuthClient{
		baseURL: authServiceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckReportAuthorization checks if reports are authorized for a user
func (ac *ReportAuthClient) CheckReportAuthorization(bearerToken string, reportSeqs []int) ([]models.ReportAuthorization, error) {
	if len(reportSeqs) == 0 {
		return []models.ReportAuthorization{}, nil
	}

	// Create request payload
	req := models.ReportAuthorizationRequest{
		ReportSeqs: reportSeqs,
	}

	// Marshal request to JSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal authorization request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/v3/reports/authorization", ac.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))

	// Make request
	resp, err := ac.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make authorization request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authorization request failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var authResp models.ReportAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to decode authorization response: %w", err)
	}

	log.Printf("DEBUG: Authorization check completed for %d reports, got %d authorizations", len(reportSeqs), len(authResp.Authorizations))

	return authResp.Authorizations, nil
}

// FilterAuthorizedReports filters reports based on authorization response
func (ac *ReportAuthClient) FilterAuthorizedReports(reports []models.ReportWithAnalysis, authorizations []models.ReportAuthorization) []models.ReportWithAnalysis {
	// Create a map of authorized report sequences for quick lookup
	authorizedSeqs := make(map[int]bool)
	for _, auth := range authorizations {
		if auth.Authorized {
			authorizedSeqs[auth.ReportSeq] = true
		}
	}

	// Filter reports to only include authorized ones
	var authorizedReports []models.ReportWithAnalysis
	for _, report := range reports {
		if authorizedSeqs[report.Report.Seq] {
			authorizedReports = append(authorizedReports, report)
		}
	}

	log.Printf("DEBUG: Filtered %d reports to %d authorized reports", len(reports), len(authorizedReports))
	return authorizedReports
}
