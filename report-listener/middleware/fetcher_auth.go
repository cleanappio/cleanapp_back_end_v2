package middleware

import (
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"report-listener/database"
)

// FetcherAuthMiddleware authenticates fetchers using a static Bearer token validated against the fetchers table.
func FetcherAuthMiddleware(db *database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
			c.Abort()
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		sum := sha256.Sum256([]byte(token))

		fetcherID, err := db.ValidateFetcherToken(c.Request.Context(), sum[:])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}
		c.Set("fetcher_id", fetcherID)
		c.Next()
	}
}
