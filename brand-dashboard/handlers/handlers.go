package handlers

import (
	"log"
	"strconv"

	"brand-dashboard/middleware"
	"brand-dashboard/models"
	"brand-dashboard/services"
	"brand-dashboard/utils"

	"github.com/gin-gonic/gin"
)

// BrandHandler handles HTTP requests for brand-related endpoints
type BrandHandler struct {
	databaseService *services.DatabaseService
}

// NewBrandHandler creates a new brand handler
func NewBrandHandler(databaseService *services.DatabaseService) *BrandHandler {
	return &BrandHandler{
		databaseService: databaseService,
	}
}

// HealthHandler handles health check requests
func (h *BrandHandler) HealthHandler(c *gin.Context) {
	response := models.HealthResponse{
		Status:  "healthy",
		Message: "Brand Dashboard service is running",
		Service: "brand-dashboard",
	}
	c.JSON(200, response)
}

// BrandsHandler handles requests for available brands
func (h *BrandHandler) BrandsHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: Brands request from user %s", userID)

	brandsInfo, err := h.databaseService.GetBrandsInfo()
	if err != nil {
		log.Printf("Error getting brands info: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.BrandsResponse{
		Brands: brandsInfo,
		Count:  len(brandsInfo),
	}
	c.JSON(200, response)
}

// ReportsHandler handles requests for reports with brand analysis
func (h *BrandHandler) ReportsHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: Reports request from user %s", userID)

	brandName := c.Query("brand")
	nStr := c.Query("n")

	if brandName == "" {
		c.JSON(400, gin.H{"error": "brand parameter is required"})
		return
	}
	if nStr == "" {
		c.JSON(400, gin.H{"error": "n parameter is required"})
		return
	}

	n, err := strconv.Atoi(nStr)
	if err != nil {
		c.JSON(400, gin.H{"error": "n must be a valid integer"})
		return
	}
	if n <= 0 {
		c.JSON(400, gin.H{"error": "n must be greater than 0"})
		return
	}

	// Check if the brand name is configured
	normalizedBrand := utils.NormalizeBrandName(brandName)
	isMatch, matchedBrand := h.databaseService.Cfg.IsBrandMatch(normalizedBrand)

	if !isMatch {
		c.JSON(400, gin.H{"error": "brand not found or not configured"})
		return
	}

	// Get bearer token from context
	bearerToken := middleware.GetBearerTokenFromContext(c)

	reports, err := h.databaseService.GetReportsByBrand(matchedBrand, n, bearerToken)
	if err != nil {
		log.Printf("Error getting reports for brand %s: %v", brandName, err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.ReportsResponse{
		Reports: reports,
		Count:   len(reports),
		Brand:   matchedBrand,
	}
	c.JSON(200, response)
}
