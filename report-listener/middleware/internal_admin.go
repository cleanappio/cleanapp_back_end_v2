package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// InternalAdminToken protects /internal/* endpoints with a shared secret.
// Header: X-Internal-Admin-Token: <token>
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
