package handlers

import (
	"testing"
	"time"

	"report-listener/models"
)

func TestAnalyzeClusterReportsGroupsSimilarNearbyReports(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	reports := []models.ReportWithAnalysis{
		{
			Report:   models.Report{Seq: 101, Timestamp: now.Add(-2 * time.Hour), Latitude: 47.3769, Longitude: 8.5417},
			Analysis: []models.ReportAnalysis{{Language: "en", Title: "Facade bricks falling over school entrance", Summary: "Loose facade bricks above the main school entrance.", Description: "Several facade bricks are unstable over a walkway used by children.", Classification: "physical", SeverityLevel: 0.92, BrandDisplayName: "Zurich School"}},
		},
		{
			Report:   models.Report{Seq: 102, Timestamp: now.Add(-90 * time.Minute), Latitude: 47.3770, Longitude: 8.5418},
			Analysis: []models.ReportAnalysis{{Language: "en", Title: "Masonry risk above school facade", Summary: "Facade bricks look loose above the same school walkway.", Description: "Potential brick fall risk near the school entrance.", Classification: "physical", SeverityLevel: 0.88, BrandDisplayName: "Zurich School"}},
		},
		{
			Report:   models.Report{Seq: 103, Timestamp: now.Add(-30 * time.Minute), Latitude: 47.39, Longitude: 8.55},
			Analysis: []models.ReportAnalysis{{Language: "en", Title: "Broken playground fence", Summary: "Fence panel detached near sports field.", Description: "Separate hazard away from the facade issue.", Classification: "physical", SeverityLevel: 0.55, BrandDisplayName: "Zurich School"}},
		},
	}

	resp := analyzeClusterReports(reports, "physical", "geometry", 0, nil, nil)
	if resp.Stats.ReportCount != 3 {
		t.Fatalf("expected report count 3, got %d", resp.Stats.ReportCount)
	}
	if len(resp.Hypotheses) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(resp.Hypotheses))
	}
	if resp.Hypotheses[0].ReportCount != 2 {
		t.Fatalf("expected top hypothesis to contain 2 reports, got %d", resp.Hypotheses[0].ReportCount)
	}
}

func TestPreferredAnalysisPrefersEnglish(t *testing.T) {
	report := &models.ReportWithAnalysis{
		Analysis: []models.ReportAnalysis{
			{Language: "de", Title: "Deutscher Titel", SeverityLevel: 0.4, Classification: "physical"},
			{Language: "en", Title: "English Title", SeverityLevel: 0.7, Classification: "physical"},
		},
	}
	analysis := preferredAnalysis(report)
	if analysis == nil || analysis.Title != "English Title" {
		t.Fatalf("expected English analysis, got %#v", analysis)
	}
}
