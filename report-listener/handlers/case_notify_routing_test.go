package handlers

import (
	"testing"

	"report-listener/models"
)

func TestBuildNotifyPlanPrefersPrimaryContactAndSuppressesDepartmentDuplicates(t *testing.T) {
	profile := caseRoutingProfile{
		DefectClass: "physical_structural",
		HazardMode:  "emergency",
		Structural:  true,
		Severe:      true,
		Urgent:      true,
	}

	targets := []models.CaseEscalationTarget{
		{
			ID:                1,
			Organization:      "Bau und Planung",
			OrganizationKey:   "bau und planung",
			DisplayName:       "Bau und Planung",
			RoleType:          "building_authority",
			DecisionScope:     "regulator",
			Email:             "bau.planung@adliswil.ch",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.95,
			ConfidenceScore:   0.9,
			SendEligibility:   "auto",
		},
		{
			ID:                2,
			Organization:      "Bau und Planung",
			OrganizationKey:   "bau und planung",
			DisplayName:       "Damiano Maiolo",
			RoleType:          "building_authority",
			DecisionScope:     "regulator",
			Email:             "damiano.maiolo@adliswil.ch",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.95,
			ConfidenceScore:   0.9,
			SendEligibility:   "auto",
		},
		{
			ID:                3,
			Organization:      "Schule Adliswil",
			OrganizationKey:   "schule adliswil",
			DisplayName:       "School Administration",
			RoleType:          "operator",
			DecisionScope:     "site_ops",
			Email:             "schule@adliswil.ch",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.91,
			ConfidenceScore:   0.88,
			SendEligibility:   "auto",
		},
	}

	plan := buildNotifyPlan("", profile, targets, nil)
	if plan == nil {
		t.Fatalf("expected notify plan")
	}

	selected := selectedTargetIDs(plan)
	if !selected[1] {
		t.Fatalf("expected primary department inbox to be selected")
	}
	if selected[2] {
		t.Fatalf("expected duplicate same-department named contact to be held as backup")
	}
	if !selected[3] {
		t.Fatalf("expected operator contact to be selected")
	}
}

func TestBuildNotifyPlanAppliesPerOrgWaveCaps(t *testing.T) {
	profile := caseRoutingProfile{
		DefectClass: "physical_structural",
		HazardMode:  "emergency",
		Structural:  true,
		Severe:      true,
		Urgent:      true,
	}

	targets := []models.CaseEscalationTarget{
		{
			ID:                10,
			Organization:      "Metro Authority",
			OrganizationKey:   "metro authority",
			DisplayName:       "Transit operations",
			RoleType:          "transit_authority",
			DecisionScope:     "site_ops",
			Email:             "ops@metro.example",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.94,
			ConfidenceScore:   0.9,
			SendEligibility:   "auto",
		},
		{
			ID:                11,
			Organization:      "Metro Authority",
			OrganizationKey:   "metro authority",
			DisplayName:       "Facilities",
			RoleType:          "facility_manager",
			DecisionScope:     "site_ops",
			Email:             "facilities@metro.example",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.93,
			ConfidenceScore:   0.89,
			SendEligibility:   "auto",
		},
		{
			ID:                12,
			Organization:      "Metro Authority",
			OrganizationKey:   "metro authority",
			DisplayName:       "Station Lead",
			RoleType:          "operator",
			DecisionScope:     "site_ops",
			Email:             "lead@metro.example",
			AttributionClass:  "official_direct",
			ActionabilityScore: 0.92,
			ConfidenceScore:   0.88,
			SendEligibility:   "auto",
		},
	}

	plan := buildNotifyPlan("", profile, targets, nil)
	if plan == nil {
		t.Fatalf("expected notify plan")
	}

	selected := selectedTargetIDs(plan)
	if !selected[10] || !selected[11] {
		t.Fatalf("expected first two site-ops contacts to be selected")
	}
	if selected[12] {
		t.Fatalf("expected third same-organization site-ops contact to be held back by org cap")
	}
}

func selectedTargetIDs(plan *models.CaseNotifyPlan) map[int64]bool {
	out := make(map[int64]bool)
	for _, item := range plan.Items {
		if item.Selected && item.TargetID != nil {
			out[*item.TargetID] = true
		}
	}
	return out
}
