package middleware

import (
	"net/http"
	"strings"

	"cleanapp-common/authx"
	"github.com/gin-gonic/gin"
	"report-listener/config"
	"report-listener/database"
)

// AuthMiddleware validates JWT access tokens locally against the shared auth token store.
func AuthMiddleware(cfg *config.Config, db *database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			c.Abort()
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bearer token required"})
			c.Abort()
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		result, err := authx.VerifyAccessToken(c.Request.Context(), db.DB(), token, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}
		c.Set("user_id", result.UserID)
		c.Set("bearer_token", token)
		c.Next()
	}
}

func GetUserIDFromContext(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return ""
}
