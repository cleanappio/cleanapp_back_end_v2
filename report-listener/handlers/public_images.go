package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"report-listener/publicdiscovery"
	"report-listener/publicid"

	"github.com/gin-gonic/gin"
)

const publicReportMediaMaxBytes = 6 * 1024 * 1024

func (h *Handlers) publicMediaReportID(token string) (string, bool) {
	if h.discoveryCodec == nil || len(token) > 2048 {
		return "", false
	}
	payload, err := h.discoveryCodec.Open(token)
	if err != nil || payload.Kind != publicdiscovery.KindReport ||
		(payload.Classification != "physical" && payload.Classification != "digital") ||
		!publicid.IsReportID(payload.PublicID) {
		return "", false
	}
	return payload.PublicID, true
}

// GetPublicReportMedia opens a sealed report token without spending the token
// resolver/detail quotas. Every image read rechecks current publication policy.
func (h *Handlers) GetPublicReportMedia(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("X-Content-Type-Options", "nosniff")
	publicID, valid := h.publicMediaReportID(strings.TrimSpace(c.Query("token")))
	if !valid {
		c.Status(http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	image, err := h.db.GetPublicReportMediaByPublicID(ctx, publicID, publicReportMediaMaxBytes)
	if err != nil || len(image) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, detectImageContentType(image), image)
}

func detectImageContentType(imageData []byte) string {
	if detected := http.DetectContentType(imageData); strings.HasPrefix(detected, "image/") {
		return detected
	}
	contentType := "image/jpeg"
	if len(imageData) <= 4 {
		return contentType
	}
	if imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47 {
		return "image/png"
	}
	if imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46 {
		return "image/gif"
	}
	if imageData[0] == 0x42 && imageData[1] == 0x4D {
		return "image/bmp"
	}
	if imageData[0] == 0xFF && imageData[1] == 0xD8 {
		return "image/jpeg"
	}
	return contentType
}

func (h *Handlers) GetImageByPublicID(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("X-Content-Type-Options", "nosniff")
	publicID := strings.TrimSpace(c.Query("public_id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing public_id parameter"})
		return
	}

	imageData, err := h.db.GetImageByPublicID(c.Request.Context(), publicID)
	if err != nil {
		log.Printf("Failed to get image for report public_id %s: %v", publicID, err)
		if err.Error() == fmt.Sprintf("report with public_id %s not found", publicID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	base64Image := base64.StdEncoding.EncodeToString(imageData)
	c.JSON(http.StatusOK, gin.H{"image": base64Image})
}

func (h *Handlers) GetRawImageByPublicID(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Header("X-Content-Type-Options", "nosniff")
	publicID := strings.TrimSpace(c.Query("public_id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing public_id parameter"})
		return
	}

	imageData, err := h.db.GetImageByPublicID(c.Request.Context(), publicID)
	if err != nil {
		log.Printf("Failed to get image for report public_id %s: %v", publicID, err)
		if err.Error() == fmt.Sprintf("report with public_id %s not found", publicID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve image"})
		}
		return
	}

	contentType := detectImageContentType(imageData)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(imageData)))
	c.Data(http.StatusOK, contentType, imageData)
}
