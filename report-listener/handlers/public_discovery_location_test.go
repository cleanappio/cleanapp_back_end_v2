package handlers

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"report-listener/models"
)

func TestPublicDiscoveryRetainsPhysicalReportsWithoutMapLocations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
	}{
		{"unknown sentinel", 0, 0},
		{"invalid latitude", 91, 8.55},
		{"invalid longitude", 47.37, -181},
		{"nonfinite latitude", math.NaN(), 8.55},
		{"nonfinite longitude", 47.37, math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := discoveryLocationReport("physical", tc.lat, tc.lon)
			tokenCalls := 0
			items, err := toPublicDiscoveryCards([]models.ReportWithMinimalAnalysis{row}, "en", func(classification, publicID string) (string, error) {
				tokenCalls++
				if classification != "physical" || publicID != "r_location_test" {
					t.Fatalf("wrong report token identity: %s %s", classification, publicID)
				}
				return "sealed-report-token", nil
			}, nil)
			if err != nil || len(items) != 1 || tokenCalls != 1 {
				t.Fatalf("unlocated report missing: items=%+v calls=%d err=%v", items, tokenCalls, err)
			}
			item := items[0]
			if item.Title != "Entrance blocked" || item.Summary != "A bin blocks the entrance." || item.DiscoveryToken != "sealed-report-token" || item.Classification != "physical" {
				t.Fatalf("readable report identity lost: %+v", item)
			}
			assertDiscoveryCoordinatesOmitted(t, item)
		})
	}
}

func TestPublicDiscoveryKeepsValidPhysicalCoordinatesIncludingSingleZeroAxis(t *testing.T) {
	for _, tc := range []struct {
		lat, lon float64
	}{
		{47.37, 8.55}, {0, 12}, {12, 0}, {-90, -180}, {90, 180},
	} {
		row := discoveryLocationReport("physical", tc.lat, tc.lon)
		items, err := toPublicDiscoveryCards([]models.ReportWithMinimalAnalysis{row}, "en", func(string, string) (string, error) { return "token", nil }, nil)
		if err != nil || len(items) != 1 {
			t.Fatalf("valid report missing: %+v, %v", items, err)
		}
		if items[0].Latitude == nil || items[0].Longitude == nil || *items[0].Latitude != tc.lat || *items[0].Longitude != tc.lon {
			t.Fatalf("valid location changed: %+v", items[0])
		}
	}
}

func TestPublicDiscoveryDigitalCardsRemainIndependentOfPhysicalCoordinates(t *testing.T) {
	for _, coords := range [][2]float64{{0, 0}, {47.37, 8.55}, {math.NaN(), math.Inf(1)}} {
		row := discoveryLocationReport("digital", coords[0], coords[1])
		row.Analysis[0].BrandName = "example"
		row.Analysis[0].BrandDisplayName = "Example"
		items, err := toPublicDiscoveryCards([]models.ReportWithMinimalAnalysis{row}, "en", func(classification, publicID string) (string, error) {
			if classification != "digital" || publicID != "r_location_test" {
				t.Fatalf("wrong digital identity: %s %s", classification, publicID)
			}
			return "digital-token", nil
		}, nil)
		if err != nil || len(items) != 1 || items[0].BrandName != "example" || items[0].BrandDisplay != "Example" || items[0].DiscoveryToken != "digital-token" {
			t.Fatalf("digital card changed: %+v, %v", items, err)
		}
		assertDiscoveryCoordinatesOmitted(t, items[0])
	}
}

func discoveryLocationReport(classification string, lat, lon float64) models.ReportWithMinimalAnalysis {
	return models.ReportWithMinimalAnalysis{
		Report:   models.Report{Seq: 42, PublicID: "r_location_test", Timestamp: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC), Latitude: lat, Longitude: lon},
		Analysis: []models.MinimalAnalysis{{Classification: classification, Language: "en", Title: "Entrance blocked", Summary: "A bin blocks the entrance."}},
	}
}

func assertDiscoveryCoordinatesOmitted(t *testing.T, item models.PublicDiscoveryCard) {
	t.Helper()
	if item.Latitude != nil || item.Longitude != nil {
		t.Fatalf("unknown or nonphysical location exposed: %+v", item)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("card is not valid JSON: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, present := object["latitude"]; present {
		t.Fatalf("latitude should be absent: %s", encoded)
	}
	if _, present := object["longitude"]; present {
		t.Fatalf("longitude should be absent: %s", encoded)
	}
}
