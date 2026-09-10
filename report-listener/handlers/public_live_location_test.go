package handlers

import (
	"math"
	"report-listener/models"
	"testing"
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

func TestPublicLiveSkipsUnlocatedPhysicalReportBeforeTokenCreation(t *testing.T) {
	h := &Handlers{}
	batch, err := h.BuildPublicLiveBatch([]models.ReportWithAnalysis{{
		Report:   models.Report{PublicID: "unlocated"},
		Analysis: []models.ReportAnalysis{{Classification: "physical"}},
	}})
	if err != nil || batch.Count != 0 {
		t.Fatalf("unexpected batch: %+v, %v", batch, err)
	}
}
