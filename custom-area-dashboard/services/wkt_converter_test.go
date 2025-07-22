package services

import (
	"encoding/json"
	"testing"
)

func TestWKTConverter_ConvertGeoJSONToWKT_Polygon(t *testing.T) {
	converter := NewWKTConverter()

	// Sample GeoJSON polygon
	polygonGeoJSON := json.RawMessage(`{
		"type": "Polygon",
		"coordinates": [[[0, 0], [1, 0], [1, 1], [0, 1], [0, 0]]]
	}`)

	wkt, err := converter.ConvertGeoJSONToWKT(polygonGeoJSON)
	if err != nil {
		t.Fatalf("Failed to convert polygon to WKT: %v", err)
	}

	expected := "POLYGON((0 0,0 1,1 1,1 0,0 0))"
	if wkt != expected {
		t.Errorf("Expected WKT: %s, got: %s", expected, wkt)
	}
}

func TestWKTConverter_ConvertGeoJSONToWKT_MultiPolygon(t *testing.T) {
	converter := NewWKTConverter()

	// Sample GeoJSON multi-polygon
	multiPolygonGeoJSON := json.RawMessage(`{
		"type": "MultiPolygon",
		"coordinates": [
			[[[0, 0], [1, 0], [1, 1], [0, 1], [0, 0]]],
			[[[2, 2], [3, 2], [3, 3], [2, 3], [2, 2]]]
		]
	}`)

	wkt, err := converter.ConvertGeoJSONToWKT(multiPolygonGeoJSON)
	if err != nil {
		t.Fatalf("Failed to convert multi-polygon to WKT: %v", err)
	}

	expected := "MULTIPOLYGON(((0 0,0 1,1 1,1 0,0 0)),((2 2,2 3,3 3,3 2,2 2)))"
	if wkt != expected {
		t.Errorf("Expected WKT: %s, got: %s", expected, wkt)
	}
}

func TestWKTConverter_ConvertGeoJSONToWKT_UnsupportedType(t *testing.T) {
	converter := NewWKTConverter()

	// Sample GeoJSON with unsupported type
	unsupportedGeoJSON := json.RawMessage(`{
		"type": "Point",
		"coordinates": [0, 0]
	}`)

	_, err := converter.ConvertGeoJSONToWKT(unsupportedGeoJSON)
	if err == nil {
		t.Error("Expected error for unsupported geometry type, but got none")
	}

	expectedError := "unsupported geometry type: Point"
	if err.Error() != expectedError {
		t.Errorf("Expected error: %s, got: %s", expectedError, err.Error())
	}
}

func TestWKTConverter_ConvertGeoJSONToWKT_InvalidJSON(t *testing.T) {
	converter := NewWKTConverter()

	// Invalid JSON
	invalidJSON := json.RawMessage(`{invalid json}`)

	_, err := converter.ConvertGeoJSONToWKT(invalidJSON)
	if err == nil {
		t.Error("Expected error for invalid JSON, but got none")
	}
}
