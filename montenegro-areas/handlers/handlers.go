package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"montenegro-areas/models"
	"montenegro-areas/services"
)

// AreasHandler handles HTTP requests for areas-related endpoints
type AreasHandler struct {
	areasService *services.AreasService
}

// NewAreasHandler creates a new areas handler
func NewAreasHandler(areasService *services.AreasService) *AreasHandler {
	return &AreasHandler{
		areasService: areasService,
	}
}

// HealthHandler handles health check requests
func (h *AreasHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	response := models.HealthResponse{
		Status:  "healthy",
		Message: "Montenegro Areas service is running",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// AreasByAdminLevelHandler handles requests for areas by admin level
func (h *AreasHandler) AreasByAdminLevelHandler(w http.ResponseWriter, r *http.Request) {
	// Get admin_level from query parameter
	adminLevelStr := r.URL.Query().Get("admin_level")
	if adminLevelStr == "" {
		http.Error(w, "admin_level parameter is required", http.StatusBadRequest)
		return
	}

	// Parse admin_level to int
	adminLevel, err := strconv.Atoi(adminLevelStr)
	if err != nil {
		http.Error(w, "admin_level must be a valid integer", http.StatusBadRequest)
		return
	}

	// Get areas for the specified admin level
	areas, err := h.areasService.GetAreasByAdminLevel(adminLevel)
	if err != nil {
		log.Printf("Error getting areas for admin level %d: %v", adminLevel, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create response
	response := models.AreasByAdminLevelResponse{
		AdminLevel: adminLevel,
		Count:      len(areas),
		Areas:      areas,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// AvailableAdminLevelsHandler handles requests for available admin levels
func (h *AreasHandler) AvailableAdminLevelsHandler(w http.ResponseWriter, r *http.Request) {
	levels, err := h.areasService.GetAvailableAdminLevels()
	if err != nil {
		log.Printf("Error getting available admin levels: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := models.AdminLevelsResponse{
		AdminLevels: levels,
		Count:       len(levels),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
