package services

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WKTConverter provides functions to convert GeoJSON geometries to WKT format
type WKTConverter struct{}

// NewWKTConverter creates a new WKT converter instance
func NewWKTConverter() *WKTConverter {
	return &WKTConverter{}
}

// ConvertGeoJSONToWKT converts a GeoJSON geometry to WKT format
func (w *WKTConverter) ConvertGeoJSONToWKT(geometry json.RawMessage) (string, error) {
	// Parse the GeoJSON geometry
	var geom struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(geometry, &geom); err != nil {
		return "", fmt.Errorf("failed to unmarshal geometry: %w", err)
	}

	// Convert based on geometry type
	switch geom.Type {
	case "Polygon":
		return w.convertPolygonToWKT(geom.Coordinates)
	case "MultiPolygon":
		return w.convertMultiPolygonToWKT(geom.Coordinates)
	default:
		return "", fmt.Errorf("unsupported geometry type: %s", geom.Type)
	}
}

// convertPolygonToWKT converts a GeoJSON polygon to WKT format
func (w *WKTConverter) convertPolygonToWKT(coordinates json.RawMessage) (string, error) {
	var coords [][][]float64
	if err := json.Unmarshal(coordinates, &coords); err != nil {
		return "", fmt.Errorf("failed to unmarshal polygon coordinates: %w", err)
	}

	if len(coords) == 0 {
		return "", fmt.Errorf("empty polygon coordinates")
	}

	return fmt.Sprintf("POLYGON(%s)", w.innerWKT(coords)), nil
}

// convertMultiPolygonToWKT converts a GeoJSON multi-polygon to WKT format
func (w *WKTConverter) convertMultiPolygonToWKT(coordinates json.RawMessage) (string, error) {
	var coords [][][][]float64
	if err := json.Unmarshal(coordinates, &coords); err != nil {
		return "", fmt.Errorf("failed to unmarshal multi-polygon coordinates: %w", err)
	}

	if len(coords) == 0 {
		return "", fmt.Errorf("empty multi-polygon coordinates")
	}

	// Convert each polygon
	var polygons []string
	for _, polygon := range coords {
		// Convert polygon to JSON and then to WKT
		polygonJSON, err := json.Marshal(polygon)
		if err != nil {
			return "", fmt.Errorf("failed to marshal polygon: %w", err)
		}
		polygonWKT, err := w.convertPolygonToWKT(polygonJSON)
		if err != nil {
			return "", fmt.Errorf("failed to convert polygon: %w", err)
		}
		// Remove the POLYGON wrapper and add to the list
		polygonWKT = strings.TrimPrefix(polygonWKT, "POLYGON(")
		polygonWKT = strings.TrimSuffix(polygonWKT, ")")
		polygons = append(polygons, fmt.Sprintf("(%s)", polygonWKT))
	}

	return fmt.Sprintf("MULTIPOLYGON(%s)", strings.Join(polygons, ",")), nil
}

// innerWKT converts polygon coordinates to WKT format
// This is adapted from the area_index.innerWKT function
func (w *WKTConverter) innerWKT(poly [][][]float64) string {
	wktList := make([][]string, len(poly))
	for i, loop := range poly {
		wktList[i] = make([]string, len(loop))
		for j, point := range loop {
			// WKT format: longitude latitude (note the order)
			wktList[i][j] = fmt.Sprintf("%g %g", point[1], point[0])
		}
	}
	wktLoops := make([]string, len(wktList))
	for i, wktPairs := range wktList {
		wktLoops[i] = fmt.Sprintf("(%s)", strings.Join(wktPairs, ","))
	}
	return strings.Join(wktLoops, ",")
}
