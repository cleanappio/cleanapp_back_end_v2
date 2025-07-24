package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// AuthClient handles communication with the auth-service
type AuthClient struct {
	baseURL    string
	httpClient *http.Client
}

// ValidateTokenRequest represents the request to validate a token
type ValidateTokenRequest struct {
	Token string `json:"token"`
}

// ValidateTokenResponse represents the response from token validation
type ValidateTokenResponse struct {
	Valid  bool   `json:"valid"`
	UserID string `json:"user_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NewAuthClient creates a new auth client
func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ValidateToken validates a token by calling the auth-service
func (ac *AuthClient) ValidateToken(token string) (string, error) {
	// Prepare request
	reqBody := ValidateTokenRequest{
		Token: token,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/v3/validate-token", ac.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make request
	resp, err := ac.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request to auth service: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		log.Printf("Auth service returned status %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("auth service returned status %d", resp.StatusCode)
	}

	// Parse response
	var tokenResp ValidateTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check if token is valid
	if !tokenResp.Valid {
		return "", fmt.Errorf("invalid token: %s", tokenResp.Error)
	}

	return tokenResp.UserID, nil
}
