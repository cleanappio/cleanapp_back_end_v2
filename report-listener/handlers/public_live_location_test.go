package handlers

import (
	"encoding/json"
	"math"
	"report-listener/config"
	"report-listener/models"
	"report-listener/publicdiscovery"
	"testing"
	"time"
)

func TestMapLocation(t *testing.T) {
	for _, tc := range []struct {
		lat, lon float64
		valid    bool
	}{
		{0, 0, false}, {0, 12, true}, {12, 0, true}, {-37.65, 145.52, true},
		{91, 12, false}, {12, 181, false}, {math.NaN(), 12, false}, {12, math.Inf(1), false},
	} {
		if hasMapLocation(tc.lat, tc.lon) != tc.valid {
			t.Errorf("location %v,%v", tc.lat, tc.lon)
		}
	}
}

func TestPublicLiveAndDiscoveryKeepSameReportWithoutInventingLocation(t *testing.T) {
	codec, err := publicdiscovery.NewCodec("live-location-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{discoveryCodec: codec, cfg: &config.Config{PublicDiscoveryTokenTTL: time.Hour}}
	for _, tc := range []struct {
		name, classification string
		lat, lon             float64
		located              bool
	}{
		{"unknown physical location", "physical", 0, 0, false},
		{"invalid physical latitude", "physical", 91, 12, false},
		{"invalid physical longitude", "physical", 12, 181, false},
		{"nonfinite physical latitude", "physical", math.NaN(), 12, false},
		{"nonfinite physical longitude", "physical", 12, math.Inf(1), false},
		{"known physical location", "physical", 47.37, 8.55, true},
		{"equator location", "physical", 0, 12, true},
		{"digital without location", "digital", 0, 0, false},
		{"digital with coordinates", "digital", 47.37, 8.55, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := discoveryLocationReport(tc.classification, tc.lat, tc.lon)
			cards, err := toPublicDiscoveryCards([]models.ReportWithMinimalAnalysis{row}, "en", h.sealDiscoveryReportToken, nil)
			if err != nil || len(cards) != 1 {
				t.Fatalf("REST discovery lost observation: %+v, %v", cards, err)
			}
			batch, err := h.BuildPublicLiveBatch([]models.ReportWithAnalysis{{Report: row.Report, Analysis: []models.ReportAnalysis{{Classification: tc.classification, Language: "en", Title: row.Analysis[0].Title, Summary: row.Analysis[0].Summary}}}})
			if err != nil || batch.Count != 1 || len(batch.Reports) != 1 {
				t.Fatalf("live discovery lost observation: %+v, %v", batch, err)
			}
			live := batch.Reports[0]
			if len(live.Analysis) != 1 || live.Analysis[0].Title != cards[0].Title || live.Analysis[0].Summary != cards[0].Summary || live.Analysis[0].Classification != cards[0].Classification {
				t.Fatalf("REST/live report content differs: %+v, %+v", cards[0], live)
			}
			for _, token := range []string{cards[0].DiscoveryToken, live.DiscoveryToken} {
				payload, err := codec.Open(token)
				if err != nil || payload.Kind != publicdiscovery.KindReport || payload.PublicID != row.Report.PublicID || payload.Classification != tc.classification {
					t.Fatalf("report identity changed in discovery token: %+v, %v", payload, err)
				}
			}
			if tc.located {
				if live.Latitude == nil || live.Longitude == nil || *live.Latitude != tc.lat || *live.Longitude != tc.lon || cards[0].Latitude == nil || cards[0].Longitude == nil || *cards[0].Latitude != *live.Latitude || *cards[0].Longitude != *live.Longitude {
					t.Fatal("REST/live known location changed")
				}
			} else {
				if live.Latitude != nil || live.Longitude != nil {
					t.Fatal("unknown or digital location exposed")
				}
				assertDiscoveryCoordinatesOmitted(t, cards[0])
			}
			encoded, err := json.Marshal(live)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &object); err != nil {
				t.Fatal(err)
			}
			if !tc.located && (object["latitude"] != nil || object["longitude"] != nil) {
				t.Fatal("unknown live coordinates must be absent from JSON")
			}
		})
	}
}
