package middleware

import (
	"cleanapp-common/authx"
	"cleanapp-common/edge"
	"customer-service/config"
	"customer-service/database"
	"database/sql"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens locally against the shared auth token store.
func AuthMiddleware(cfg *config.Config, db *sql.DB) gin.HandlerFunc {
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

		result, err := authx.VerifyAccessToken(c.Request.Context(), db, tokenString, cfg.JWTSecret)
		if err != nil {
			log.Printf("ERROR: Failed to validate token from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}
		c.Set("customer_id", result.UserID)
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

// RequireSubscription middleware checks if customer has active subscription
func RequireSubscription(service *database.CustomerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		customerID := c.GetString("customer_id")
		if customerID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func RateLimitMiddleware(rps float64, burst int) gin.HandlerFunc {
	return edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: rps, Burst: burst})
}

func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return edge.CORSMiddleware(edge.CORSConfig{AllowedOrigins: allowedOrigins, AllowCredentials: true})
}

func SecurityHeaders() gin.HandlerFunc {
	return edge.SecurityHeaders()
}
