package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"report-listener/models"
)

// Raw reader objects must never bypass the public privacy projection when its
// evidence query fails. A retryable response also prevents indexing that error.
func attachPublicDetailEvidence(c *gin.Context, report *models.ReportWithAnalysis, load func(context.Context, []*models.ReportWithAnalysis) error) bool {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	if err := load(c.Request.Context(), []*models.ReportWithAnalysis{report}); err != nil {
		c.Header("Retry-After", "60")
		c.Header("X-Robots-Tag", "noindex")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Public report temporarily unavailable"})
		return false
	}
	return true
}
