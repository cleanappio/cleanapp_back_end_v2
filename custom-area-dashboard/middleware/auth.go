package middleware

import (
	"database/sql"
	"net/http"
	"strings"

	"cleanapp-common/authx"
	"custom-area-dashboard/config"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens locally against the shared auth token store.
func AuthMiddleware(cfg *config.Config, db *sql.DB) gin.HandlerFunc {
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

		result, err := authx.VerifyAccessToken(c.Request.Context(), db, tokenString, cfg.JWTSecret)
		if err != nil {
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

func GetUserIDFromContext(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return ""
}

func GetTokenFromContext(c *gin.Context) string {
	if token, exists := c.Get("token"); exists {
		if t, ok := token.(string); ok {
			return t
		}
	}
	return ""
}
