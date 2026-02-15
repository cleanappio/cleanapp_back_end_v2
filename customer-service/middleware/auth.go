package middleware

import (
	"bytes"
	"context"
	"customer-service/config"
	"customer-service/database"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

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
		valid, customerID, err := validateTokenWithAuthService(c.Request.Context(), tokenString, cfg.AuthServiceURL)
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

		log.Printf("DEBUG: Token validated successfully for customer %s from %s", customerID, c.ClientIP())
		c.Set("customer_id", customerID)
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

	log.Printf("DEBUG: Calling auth-service to validate token: %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("ERROR: Failed to create auth-service request for token validation: %v", err)
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := authServiceHTTPClient.Do(req)
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

// RequireSubscription middleware checks if customer has active subscription
func RequireSubscription(service *database.CustomerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		customerID := c.GetString("customer_id")
		if customerID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		// Check subscription status (implement this method in service)
		// hasActive := service.HasActiveSubscription(c.Request.Context(), customerID)
		// if !hasActive {
		//     c.JSON(http.StatusPaymentRequired, gin.H{"error": "active subscription required"})
		//     c.Abort()
		//     return
		// }

		c.Next()
	}
}

// RateLimitMiddleware implements basic rate limiting
func RateLimitMiddleware() gin.HandlerFunc {
	// This is a simplified implementation
	// In production, use a proper rate limiting library with Redis
	return func(c *gin.Context) {
		// Implement rate limiting logic here
		c.Next()
	}
}

// CORSMiddleware handles CORS headers
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// SecurityHeaders adds security headers to responses
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("X-XSS-Protection", "1; mode=block")
		c.Writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Writer.Header().Set("Content-Security-Policy", "default-src 'self'")
		c.Next()
	}
}
