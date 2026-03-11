package handlers

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func detectImageContentType(imageData []byte) string {
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
