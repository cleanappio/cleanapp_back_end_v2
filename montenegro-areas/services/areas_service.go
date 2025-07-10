package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"montenegro-areas/models"
)

// AreasService manages the Montenegro areas data
type AreasService struct {
	areas    map[int][]models.MontenegroArea // admin_level -> areas
	mutex    sync.RWMutex
	loaded   bool
	filePath string
}

// NewAreasService creates a new areas service
func NewAreasService() *AreasService {
	filePath := os.Getenv("GEOJSON_FILE")
	if filePath == "" {
		filePath = "OSMB-e0b412fe96a2a2c5d8e7eb33454a21d971bea620.geojson"
	}

	return &AreasService{
		areas:    make(map[int][]models.MontenegroArea),
		filePath: filePath,
	}
}

// LoadAreas loads and parses the GeoJSON file
func (s *AreasService) LoadAreas() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check if file exists
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return fmt.Errorf("GeoJSON file not found: %s", s.filePath)
	}

	// Read the file
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to read GeoJSON file: %w", err)
	}

	// Parse the GeoJSON
	var collection models.FeatureCollection
	if err := json.Unmarshal(data, &collection); err != nil {
		return fmt.Errorf("failed to parse GeoJSON: %w", err)
	}

	// Clear existing data
	s.areas = make(map[int][]models.MontenegroArea)

	// Process each feature
	for _, feature := range collection.Features {
		area, err := s.parseFeature(feature)
		if err != nil {
			log.Printf("Warning: failed to parse feature: %v", err)
			continue
		}

		if area != nil {
			s.areas[area.AdminLevel] = append(s.areas[area.AdminLevel], *area)
		}
	}

	s.loaded = true
	log.Printf("Loaded %d admin levels with areas", len(s.areas))

	// Log counts for each admin level
	for adminLevel, areas := range s.areas {
		log.Printf("Admin level %d: %d areas", adminLevel, len(areas))
	}

	return nil
}

// parseFeature converts a GeoJSON feature to a MontenegroArea
func (s *AreasService) parseFeature(feature models.Feature) (*models.MontenegroArea, error) {
	// Extract admin_level from properties
	adminLevelRaw, exists := feature.Properties["admin_level"]
	if !exists {
		return nil, fmt.Errorf("admin_level not found in properties")
	}

	// Convert admin_level to int
	var adminLevel int
	switch v := adminLevelRaw.(type) {
	case float64:
		adminLevel = int(v)
	case int:
		adminLevel = v
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			adminLevel = parsed
		} else {
			return nil, fmt.Errorf("invalid admin_level format: %v", v)
		}
	default:
		return nil, fmt.Errorf("unexpected admin_level type: %T", v)
	}

	// Extract name
	var name string
	if nameRaw, exists := feature.Properties["name"]; exists {
		if nameStr, ok := nameRaw.(string); ok {
			name = nameStr
		}
	}

	// Extract OSM ID
	var osmID int64
	if osmIDRaw, exists := feature.Properties["osm_id"]; exists {
		switch v := osmIDRaw.(type) {
		case float64:
			osmID = int64(v)
		case int:
			osmID = int64(v)
		case int64:
			osmID = v
		}
	}

	// Convert geometry to JSON
	areaData, err := json.Marshal(feature.Geometry)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal geometry: %w", err)
	}

	return &models.MontenegroArea{
		AdminLevel: adminLevel,
		Area:       areaData,
		Name:       name,
		OSMID:      osmID,
	}, nil
}

// GetAreasByAdminLevel returns all areas for a given admin level
func (s *AreasService) GetAreasByAdminLevel(adminLevel int) ([]models.MontenegroArea, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.loaded {
		return nil, fmt.Errorf("areas not loaded yet")
	}

	areas, exists := s.areas[adminLevel]
	if !exists {
		return []models.MontenegroArea{}, nil // Return empty slice if no areas found
	}

	return areas, nil
}

// GetAvailableAdminLevels returns all available admin levels
func (s *AreasService) GetAvailableAdminLevels() ([]int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if !s.loaded {
		return nil, fmt.Errorf("areas not loaded yet")
	}

	levels := make([]int, 0, len(s.areas))
	for level := range s.areas {
		levels = append(levels, level)
	}

	return levels, nil
}

// IsLoaded returns whether the areas have been loaded
func (s *AreasService) IsLoaded() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.loaded
}
