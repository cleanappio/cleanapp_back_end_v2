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

// ReportsHandler handles requests for reports within a MontenegroArea
func (h *AreasHandler) ReportsHandler(w http.ResponseWriter, r *http.Request) {
	// Get query parameters
	osmIDStr := r.URL.Query().Get("osm_id")
	nStr := r.URL.Query().Get("n")

	if osmIDStr == "" {
		http.Error(w, "osm_id parameter is required", http.StatusBadRequest)
		return
	}

	if nStr == "" {
		http.Error(w, "n parameter is required", http.StatusBadRequest)
		return
	}

	// Parse OSM ID
	osmID, err := strconv.ParseInt(osmIDStr, 10, 64)
	if err != nil {
		http.Error(w, "osm_id must be a valid integer", http.StatusBadRequest)
		return
	}

	// Parse number of reports
	n, err := strconv.Atoi(nStr)
	if err != nil {
		http.Error(w, "n must be a valid integer", http.StatusBadRequest)
		return
	}

	if n <= 0 {
		http.Error(w, "n must be greater than 0", http.StatusBadRequest)
		return
	}

	// Get reports from database
	reports, err := h.databaseService.GetReportsByMontenegroArea(osmID, n)
	if err != nil {
		log.Printf("Error getting reports for OSM ID %d: %v", osmID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create response
	response := models.ReportsResponse{
		Reports: reports,
		Count:   len(reports),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ReportsAggrHandler handles requests for aggregated reports data for admin level 6 areas
func (h *AreasHandler) ReportsAggrHandler(w http.ResponseWriter, r *http.Request) {
	// Get aggregated reports data for all areas of admin level 6
	areasData, err := h.databaseService.GetReportsAggregatedData()
	if err != nil {
		log.Printf("Error getting aggregated reports data: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create response
	response := models.ReportsAggrResponse{
		Areas: areasData,
		Count: len(areasData),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
