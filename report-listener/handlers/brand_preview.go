package handlers

import (
	"context"
	"net/http"
	"strconv"

	"report-listener/database"
	"report-listener/models"

	"github.com/gin-gonic/gin"
)

func servePublicBrandPreview(c *gin.Context, load func(context.Context, database.EvidenceFilter) (models.PublicBrandPreview, error)) {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("X-Robots-Tag", "noindex")
	n, err := strconv.Atoi(c.DefaultQuery("n", "6"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "n must be a positive integer"})
		return
	}
	filter, err := database.NormalizeBrandPreviewFilter(database.EvidenceFilter{
		BrandName: c.Query("brand_name"), Language: c.DefaultQuery("lang", "en"),
		Kind: c.DefaultQuery("evidence_kind", "firsthand"), Limit: n,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid brand preview parameters"})
		return
	}
	result, err := load(c.Request.Context(), filter)
	if err != nil {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Brand preview temporarily unavailable"})
		return
	}
	if !result.BrandFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "No public reports found for this brand", "brand_found": false})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handlers) GetPublicBrandPreview(c *gin.Context) {
	servePublicBrandPreview(c, h.db.GetPublicBrandPreview)
}
