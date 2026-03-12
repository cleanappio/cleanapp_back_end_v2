package handlers

import (
	"testing"

	"report-listener/models"
)

func TestRouteReportEscalationTargetsPrioritizesAuthoritiesForStructuralHazards(t *testing.T) {
	report := &models.ReportWithAnalysis{
		Report: models.Report{Seq: 42, PublicID: "rpt_test"},
		Analysis: []models.ReportAnalysis{
			{
				Title:          "Structural crack in load-bearing metro support column",
				Summary:        "A severe crack in a support column at a metro station creates falling concrete risk near commuters.",
				Description:    "Immediate danger to passengers near a transit platform.",
				SeverityLevel:  0.95,
				Classification: "physical",
			},
		},
	}
	targets := []models.CaseEscalationTarget{
		{
			RoleType:        "building_authority",
			DisplayName:     "Municipal Building Authority",
			Email:           "building@authority.example",
			TargetSource:    "official_directory",
			Verification:    "official_page",
			ConfidenceScore: 0.92,
		},
		{
			RoleType:        "contractor",
			DisplayName:     "Original Contractor",
			Email:           "contractor@example.com",
			TargetSource:    "search_discovery",
			Verification:    "directory_listing",
			ConfidenceScore: 0.76,
		},
	}

	routed := routeReportEscalationTargets(report, targets)
	if len(routed) != 2 {
		t.Fatalf("expected 2 routed targets, got %d", len(routed))
	}
	if routed[0].DecisionScope != "regulator" {
		t.Fatalf("expected first target to be regulator, got %q", routed[0].DecisionScope)
	}
	if routed[0].NotifyTier != 1 {
		t.Fatalf("expected authority target in wave 1 for emergency structural hazard, got tier %d", routed[0].NotifyTier)
	}
	if routed[0].SendEligibility == "hold" {
		t.Fatalf("expected authority target to be actionable, got hold")
	}
	if routed[1].DecisionScope != "project_party" {
		t.Fatalf("expected second target to be project_party, got %q", routed[1].DecisionScope)
	}
}
