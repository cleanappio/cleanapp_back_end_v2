package middleware

import (
	"net/http"
	"strings"

	"auth-service/database"
	"cleanapp-common/edge"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens for protected routes
func AuthMiddleware(service *database.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		// Extract token from Bearer scheme
		tokenString := extractToken(authHeader)
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		// Validate token
		userID, err := service.ValidateToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		// Set user ID in context for use in handlers
		c.Set("user_id", userID)
		c.Set("token", tokenString)
		c.Next()
	}
}

// extractToken extracts the token from the Authorization header
func extractToken(authHeader string) string {
	// Check for Bearer scheme
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	return parts[1]
}

// RateLimitMiddleware implements basic rate limiting
func RateLimitMiddleware(rps float64, burst int) gin.HandlerFunc {
	return edge.RateLimitMiddleware(edge.RateLimitConfig{
		RPS:   rps,
		Burst: burst,
	})
}

// CORSMiddleware handles CORS headers
func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return edge.CORSMiddleware(edge.CORSConfig{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	})
}

// SecurityHeaders adds security headers to responses
func SecurityHeaders() gin.HandlerFunc {
	return edge.SecurityHeaders()
}
