package middleware

import (
	"database/sql"
	"net/http"
	"strings"

	"brand-dashboard/config"
	"cleanapp-common/authx"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens locally against the shared auth token store.
func AuthMiddleware(cfg *config.Config, db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			c.Abort()
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		result, err := authx.VerifyAccessToken(c.Request.Context(), db, token, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
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

func GetBearerTokenFromContext(c *gin.Context) string {
	if token, exists := c.Get("bearer_token"); exists {
		if t, ok := token.(string); ok {
			return t
		}
	}
	return ""
}
