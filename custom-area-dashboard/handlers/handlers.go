package handlers

import (
	"log"
	"strconv"

	"custom-area-dashboard/middleware"
	"custom-area-dashboard/models"
	"custom-area-dashboard/services"

	"github.com/gin-gonic/gin"
)

// AreasHandler handles HTTP requests for areas-related endpoints
type AreasHandler struct {
	areasService    *services.AreasService
	databaseService *services.DatabaseService
}

// NewAreasHandler creates a new areas handler
func NewAreasHandler(areasService *services.AreasService, databaseService *services.DatabaseService) *AreasHandler {
	return &AreasHandler{
		areasService:    areasService,
		databaseService: databaseService,
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
func (h *AreasHandler) AreasByAdminLevelHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: AreasByAdminLevel request from user %s", userID)

	adminLevelStr := c.Query("admin_level")
	if adminLevelStr == "" {
		c.JSON(400, gin.H{"error": "admin_level parameter is required"})
		return
	}

	adminLevel, err := strconv.Atoi(adminLevelStr)
	if err != nil {
		c.JSON(400, gin.H{"error": "admin_level must be a valid integer"})
		return
	}

	areas, err := h.areasService.GetAreasByAdminLevel(adminLevel)
	if err != nil {
		log.Printf("Error getting areas for admin level %d: %v", adminLevel, err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.AreasByAdminLevelResponse{
		AdminLevel: adminLevel,
		Count:      len(areas),
		Areas:      areas,
	}
	c.JSON(200, response)
}

// AvailableAdminLevelsHandler handles requests for available admin levels
func (h *AreasHandler) AvailableAdminLevelsHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: AvailableAdminLevels request from user %s", userID)

	levels, err := h.areasService.GetAvailableAdminLevels()
	if err != nil {
		log.Printf("Error getting available admin levels: %v", err)
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	response := models.AdminLevelsResponse{
		AdminLevels: levels,
		Count:       len(levels),
	}
	c.JSON(200, response)
}

// ReportsHandler handles requests for reports within a custom area
func (h *AreasHandler) ReportsHandler(c *gin.Context) {
	userID := middleware.GetUserIDFromContext(c)
	log.Printf("INFO: Reports request from user %s", userID)

	osmIDStr := c.Query("osm_id")
	nStr := c.Query("n")

	if osmIDStr == "" {
		c.JSON(400, gin.H{"error": "osm_id parameter is required"})
		return
	}
	if nStr == "" {
		c.JSON(400, gin.H{"error": "n parameter is required"})
		return
	}

	osmID, err := strconv.ParseInt(osmIDStr, 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "osm_id must be a valid integer"})
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

	reports, err := h.databaseService.GetReportsByCustomArea(osmID, n)
	if err != nil {
		log.Printf("Error getting reports for OSM ID %d: %v", osmID, err)
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

	areasData, err := h.databaseService.GetReportsAggregatedData()
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
