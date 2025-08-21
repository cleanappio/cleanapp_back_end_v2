package handlers

import (
	"log"
	"strconv"

	"custom-area-dashboard/config"
	"custom-area-dashboard/middleware"
	"custom-area-dashboard/models"
	"custom-area-dashboard/services"

	"github.com/gin-gonic/gin"
)

// AreasHandler handles HTTP requests for areas-related endpoints
type AreasHandler struct {
	databaseService *services.DatabaseService
	cfg             *config.Config
}

// NewAreasHandler creates a new areas handler
func NewAreasHandler(databaseService *services.DatabaseService, cfg *config.Config) *AreasHandler {
	return &AreasHandler{
		databaseService: databaseService,
		cfg:             cfg,
	}
}

// HealthHandler handles health check requests
func (h *AreasHandler) HealthHandler(c *gin.Context) {
	response := models.HealthResponse{
		Status:  "healthy",
		Message: "Custom Area Dashboard service is running",
	}
	c.JSON(200, response)
}

// AreasByAdminLevelHandler handles requests for areas by admin level
func (h *AreasHandler) AreasHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: AreasByAdminLevel request from user %s", userID)

	areas, err := h.databaseService.GetAreasByIds([]int64{h.cfg.CustomAreaID})
	if err != nil {
		log.Printf("Error getting areas: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.AreasResponse{
		Count: len(areas),
		Areas: areas,
	}
	c.JSON(200, response)
}

func (h *AreasHandler) SubAreasHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: AreasByAdminLevel request from user %s", userID)

	areas, err := h.databaseService.GetAreasByIds(h.cfg.CustomAreaSubIDs)
	if err != nil {
		log.Printf("Error getting areas: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.AreasResponse{
		Count: len(areas),
		Areas: areas,
	}
	c.JSON(200, response)
}

// ReportsHandler handles requests for reports within a custom area
func (h *AreasHandler) ReportsHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: Reports request from user %s", userID)

	nStr := c.Query("n")

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

	reports, err := h.databaseService.GetReportsByCustomArea(n, userID)
	if err != nil {
		log.Printf("Error getting reports for custom area: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.ReportsResponse{
		Reports: reports,
		Count:   len(reports),
	}
	c.JSON(200, response)
}

// ReportsAggrHandler handles requests for aggregated reports data for admin level 6 areas
func (h *AreasHandler) ReportsAggrHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: ReportsAggr request from user %s", userID)

	areasData, err := h.databaseService.GetReportsAggregatedData(userID)
	if err != nil {
		log.Printf("Error getting aggregated reports data: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.ReportsAggrResponse{
		Areas: areasData,
		Count: len(areasData),
	}
	c.JSON(200, response)
}
