package handlers

import (
	"areas-service/database"
	"areas-service/models"
	"fmt"
	"net/http"
	"strconv"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type AreasHandler struct {
	areasService *database.AreasService
}

func NewAreasHandler(areasService *database.AreasService) *AreasHandler {
	return &AreasHandler{
		areasService: areasService,
	}
}

// HealthCheck returns a simple health status
func (h *AreasHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "areas-service",
	})
}

func (h *AreasHandler) CreateOrUpdateArea(c *gin.Context) {
	args := &models.CreateAreaRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /create_area call: %w", err)
		return
	}

	err := h.areasService.CreateOrUpdateArea(c.Request.Context(), args)
	if err != nil {
		log.Errorf("Error creating or updating area: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}

	c.Status(http.StatusOK)
}

func (h *AreasHandler) GetAreas(c *gin.Context) {
	latMinStr, hasLatMin := c.GetQuery("sw_lat")
	lonMinStr, hasLonMin := c.GetQuery("sw_lon")
	latMaxStr, hasLatMax := c.GetQuery("ne_lat")
	lonMaxStr, hasLonMax := c.GetQuery("ne_lon")

	var latMin, lonMin, latMax, lonMax float64
	var err error
	var vp *models.ViewPort
	if hasLatMin && hasLatMax && hasLonMin && hasLonMax {
		if latMin, err = strconv.ParseFloat(latMinStr, 64); err != nil {
			log.Errorf("Error in parsing sw_lat param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing sw_lat: %v", err))
			return
		}
		if lonMin, err = strconv.ParseFloat(lonMinStr, 64); err != nil {
			log.Errorf("Error in parsing sw_lon param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing sw_lon: %v", err))
			return
		}
		if latMax, err = strconv.ParseFloat(latMaxStr, 64); err != nil {
			log.Errorf("Error in parsing ne_lat param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing ne_lat: %v", err))
			return
		}
		if lonMax, err = strconv.ParseFloat(lonMaxStr, 64); err != nil {
			log.Errorf("Error in parsing ne_lon param: %w", err)
			c.String(http.StatusBadRequest, fmt.Sprintf("Parsing ne_lon: %v", err))
			return
		}
		vp = &models.ViewPort{
			LatMin: latMin,
			LonMin: lonMin,
			LatMax: latMax,
			LonMax: lonMax,
		}
	}

	areaIds, err := h.areasService.GetAreaIdsForViewport(c.Request.Context(), vp)
	if err != nil {
		log.Errorf("Error getting area IDs for viewport %v: %w", vp, err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("Getting area IDs: %v", err))
		return
	}

	res, err := h.areasService.GetAreas(c.Request.Context(), areaIds)
	if err != nil {
		log.Errorf("Error getting areas: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("Getting areas: %v", err))
		return
	}

	// Convert []*models.Area to []models.Area for response
	areas := make([]models.Area, len(res))
	for i, area := range res {
		areas[i] = *area
	}

	c.IndentedJSON(http.StatusOK, &models.AreasResponse{
		Areas: areas,
	})
}

func (h *AreasHandler) UpdateConsent(c *gin.Context) {
	args := &models.UpdateConsentRequest{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /update_consent call: %w", err)
		return
	}

	if err := h.areasService.UpdateConsent(c.Request.Context(), args); err != nil {
		log.Errorf("Error updating email consent: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}

	c.Status(http.StatusOK)
}

func (h *AreasHandler) GetAreasCount(c *gin.Context) {
	cnt, err := h.areasService.GetAreasCount(c.Request.Context())
	if err != nil {
		log.Errorf("Error getting areas.count: %w", err)
		c.String(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}

	c.IndentedJSON(http.StatusOK, &models.AreasCountResponse{
		Count: cnt,
	})
}
