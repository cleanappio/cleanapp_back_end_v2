package middleware

import (
	"areas-service/config"
	"database/sql"
	"net/http"
	"strings"

	"cleanapp-common/authx"
	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens locally against the shared auth token store.
func AuthMiddleware(cfg *config.Config, db *sql.DB) gin.HandlerFunc {
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

		result, err := authx.VerifyAccessToken(c.Request.Context(), db, tokenString, cfg.JWTSecret)
		if err != nil {
			log.Warnf("Token validation failed from %s: %v", c.ClientIP(), err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("user_id", result.UserID)
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
