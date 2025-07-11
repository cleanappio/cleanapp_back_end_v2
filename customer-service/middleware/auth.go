package middleware

import (
	"bytes"
	"customer-service/config"
	"customer-service/database"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens for protected routes by calling auth-service
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		tokenString := extractToken(authHeader)
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		// Call auth-service to validate token
		valid, customerID, err := validateTokenWithAuthService(tokenString, cfg.AuthServiceURL)
		if err != nil || !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

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

func validateTokenWithAuthService(token string, authServiceURL string) (bool, string, error) {
	url := authServiceURL + "/api/v3/validate-token"
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	var result struct {
		Valid      bool   `json:"valid"`
		UserID     string `json:"user_id"`
		CustomerID string `json:"customer_id"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", err
	}

	// Accept either user_id or customer_id for compatibility
	id := result.CustomerID
	if id == "" {
		id = result.UserID
	}

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
