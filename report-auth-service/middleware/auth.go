package middleware

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
// It also supports internal service communication via X-User-ID header
// If no authentication is provided, the request continues with empty user_id
func AuthMiddleware(authServiceURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for internal service communication header first
		internalUserID := c.GetHeader("X-User-ID")
		if internalUserID != "" {
			log.Printf("DEBUG: Internal service communication detected for user %s from %s", internalUserID, c.ClientIP())
			c.Set("user_id", internalUserID)
			c.Next()
			return
		}

		// Standard JWT token validation
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No authentication provided - continue with empty user_id for public access
			log.Printf("DEBUG: No authentication provided from %s - allowing public access", c.ClientIP())
			c.Set("user_id", "")
			c.Next()
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
		valid, userID, err := validateTokenWithAuthService(tokenString, authServiceURL)
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
		Valid  bool   `json:"valid"`
		UserID string `json:"user_id"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("ERROR: Failed to decode auth-service response: %v", err)
		return false, "", err
	}

	log.Printf("DEBUG: Token validation result - Valid: %t, UserID: %s", result.Valid, result.UserID)
	return result.Valid, result.UserID, nil
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func CORSMiddleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})
}

// SecurityHeaders adds security-related HTTP headers
func SecurityHeaders() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})
}

// RateLimitMiddleware implements basic rate limiting
func RateLimitMiddleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Basic rate limiting - in production, use a proper rate limiter
		// For now, just pass through
		c.Next()
	})
}
