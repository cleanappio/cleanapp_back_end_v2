package models

import (
	"encoding/json"
)

// GeoJSON structures
type FeatureCollection struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

type Feature struct {
	Type       string                 `json:"type"`
	Geometry   Geometry               `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// MontenegroArea represents a parsed area from the GeoJSON
type MontenegroArea struct {
	AdminLevel int             `json:"admin_level"`
	Area       json.RawMessage `json:"area"` // Raw geometry data
	Name       string          `json:"name,omitempty"`
	OSMID      int64           `json:"osm_id,omitempty"`
}

// AreasByAdminLevelResponse represents the response for areas by admin level
type AreasByAdminLevelResponse struct {
	AdminLevel int              `json:"admin_level"`
	Count      int              `json:"count"`
	Areas      []MontenegroArea `json:"areas"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// AdminLevelsResponse represents the response for available admin levels
type AdminLevelsResponse struct {
	AdminLevels []int `json:"admin_levels"`
	Count       int   `json:"count"`
}
