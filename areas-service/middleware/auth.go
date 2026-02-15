package middleware

import (
	"areas-service/config"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

var authServiceHTTPClient = &http.Client{
	Timeout: 6 * time.Second,
}

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Warnf("Missing authorization header from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		tokenString := extractToken(authHeader)
		if tokenString == "" {
			log.Warnf("Invalid authorization format from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		log.Debugf("Validating token from %s", c.ClientIP())

		// Call auth-service to validate token
		valid, userID, err := validateTokenWithAuthService(c.Request.Context(), tokenString, cfg.AuthServiceURL)
		if err != nil {
			log.Errorf("Failed to validate token with auth-service from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		if !valid {
			log.Warnf("Invalid token from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		log.Debugf("Token validated successfully for user %s from %s", userID, c.ClientIP())
		c.Set("user_id", userID)
		c.Set("token", tokenString)
		c.Next()
	}
}

func extractToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

func validateTokenWithAuthService(ctx context.Context, token string, authServiceURL string) (bool, string, error) {
	url := authServiceURL + "/api/v3/validate-token"
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	log.Debugf("Calling auth-service to validate token: %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		log.Errorf("Failed to create auth-service request for token validation: %v", err)
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := authServiceHTTPClient.Do(req)
	if err != nil {
		log.Errorf("Failed to call auth-service for token validation: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()

	log.Debugf("Auth-service token validation response: %d", resp.StatusCode)

	var result struct {
		Valid      bool   `json:"valid"`
		UserID     string `json:"user_id"`
		CustomerID string `json:"customer_id"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Errorf("Failed to decode auth-service response: %v", err)
		return false, "", err
	}

	// Accept either user_id or customer_id for compatibility
	id := result.CustomerID
	if id == "" {
		id = result.UserID
	}

	log.Debugf("Token validation result - Valid: %t, ID: %s", result.Valid, id)
	return result.Valid, id, nil
}
