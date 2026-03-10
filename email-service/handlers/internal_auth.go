package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func InternalAdminToken(token string) gin.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(c *gin.Context) {
		if token == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "internal admin token not configured"})
			c.Abort()
			return
		}
		got := strings.TrimSpace(c.GetHeader("X-Internal-Admin-Token"))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}
