package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"brand-dashboard/config"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens and extracts user information
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Printf("WARNING: Request without Authorization header from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Check if it's a Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("WARNING: Invalid Authorization header format from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			c.Abort()
			return
		}

		// Extract the token
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Validate the token with the auth service
		userID, err := validateTokenWithAuthService(token, cfg.AuthServiceURL)
		if err != nil {
			log.Printf("WARNING: Token validation failed from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Store user ID and bearer token in context
		c.Set("user_id", userID)
		c.Set("bearer_token", token)
		c.Next()
	}
}

// validateTokenWithAuthService validates a JWT token with the auth service
func validateTokenWithAuthService(token, authServiceURL string) (string, error) {
	// Create request payload
	requestBody := map[string]string{
		"token": token,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create request to auth service
	url := fmt.Sprintf("%s/api/v3/validate-token", authServiceURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call auth service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth service returned status %d", resp.StatusCode)
	}

	// Parse response
	var response struct {
		Valid  bool   `json:"valid"`
		UserID string `json:"user_id"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode auth service response: %w", err)
	}

	if !response.Valid {
		return "", fmt.Errorf("token validation failed: %s", response.Error)
	}

	return response.UserID, nil
}

// GetUserIDFromContext extracts user ID from Gin context
func GetUserIDFromContext(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return ""
}

// GetBearerTokenFromContext extracts the bearer token from Gin context
func GetBearerTokenFromContext(c *gin.Context) string {
	if token, exists := c.Get("bearer_token"); exists {
		if t, ok := token.(string); ok {
			return t
		}
	}
	return ""
}
