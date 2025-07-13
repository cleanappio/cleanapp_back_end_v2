package middleware

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"montenegro-areas/config"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Printf("WARNING: Missing authorization header from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		tokenString := extractToken(authHeader)
		if tokenString == "" {
			log.Printf("WARNING: Invalid authorization format from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		log.Printf("DEBUG: Validating token from %s", c.ClientIP())

		// Call auth-service to validate token
		valid, userID, err := validateTokenWithAuthService(tokenString, cfg.AuthServiceURL)
		if err != nil {
			log.Printf("ERROR: Failed to validate token with auth-service from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		if !valid {
			log.Printf("WARNING: Invalid token from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		log.Printf("DEBUG: Token validated successfully for user %s from %s", userID, c.ClientIP())
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

func validateTokenWithAuthService(token string, authServiceURL string) (bool, string, error) {
	url := authServiceURL + "/api/v3/validate-token"
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	log.Printf("DEBUG: Calling auth-service to validate token: %s", url)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("ERROR: Failed to call auth-service for token validation: %v", err)
		return false, "", err
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Auth-service token validation response: %d", resp.StatusCode)

	var result struct {
		Valid      bool   `json:"valid"`
		UserID     string `json:"user_id"`
		CustomerID string `json:"customer_id"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("ERROR: Failed to decode auth-service response: %v", err)
		return false, "", err
	}

	// Accept either user_id or customer_id for compatibility
	id := result.CustomerID
	if id == "" {
		id = result.UserID
	}

	log.Printf("DEBUG: Token validation result - Valid: %t, ID: %s", result.Valid, id)
	return result.Valid, id, nil
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

// GetTokenFromContext extracts token from Gin context
func GetTokenFromContext(c *gin.Context) string {
	if token, exists := c.Get("token"); exists {
		if t, ok := token.(string); ok {
			return t
		}
	}
	return ""
}
