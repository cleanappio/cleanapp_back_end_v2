package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TagClient handles communication with the Rust tag service
type TagClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewTagClient creates a new tag client
func NewTagClient(baseURL string) *TagClient {
	return &TagClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// AddTagsRequest represents the request to add tags to a report
type AddTagsRequest struct {
	Tags []string `json:"tags"`
}

// AddTagsResponse represents the response for adding tags
type AddTagsResponse struct {
	ReportSeq int      `json:"report_seq"`
	TagsAdded []string `json:"tags_added"`
}

// GetTagsResponse represents the response for getting tags
type GetTagsResponse struct {
	Tags []TagInfo `json:"tags"`
}

// TagInfo represents tag information
type TagInfo struct {
	ID            int64  `json:"id"`
	CanonicalName string `json:"canonical_name"`
	DisplayName   string `json:"display_name"`
	UsageCount    int64  `json:"usage_count"`
}

// AddTagsToReport adds tags to a report via the Rust service
func (tc *TagClient) AddTagsToReport(ctx context.Context, reportSeq int, tags []string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v3/reports/%d/tags", tc.baseURL, reportSeq)
	
	reqBody := AddTagsRequest{Tags: tags}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tag service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tag service returned status %d: %s", resp.StatusCode, string(body))
	}

	var response AddTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.TagsAdded, nil
}

// GetTagsForReport gets tags for a report via the Rust service
func (tc *TagClient) GetTagsForReport(ctx context.Context, reportSeq int) ([]TagInfo, error) {
	url := fmt.Sprintf("%s/api/v3/reports/%d/tags", tc.baseURL, reportSeq)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tag service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tag service returned status %d: %s", resp.StatusCode, string(body))
	}

	var response GetTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Tags, nil
}
