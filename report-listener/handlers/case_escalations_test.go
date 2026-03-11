package handlers

import (
	"strings"
	"testing"

	"report-listener/models"
)

func TestBuildCaseEscalationDraftPrefersGermanForSchulhausTargets(t *testing.T) {
	detail := &models.CaseDetail{
		Case: models.Case{
			Title:             "Incident cluster at Schulhaus Kopfholz",
			Summary:           "Case created from area scope around Schulhaus Kopfholz.",
			SeverityScore:     1,
			UrgencyScore:      1,
			LinkedReportCount: 18,
		},
		LinkedReports: []models.CaseReportLink{
			{Seq: 14034, Title: "Structural crack in concrete wall"},
		},
	}
	targets := []models.CaseEscalationTarget{
		{
			DisplayName:     "Schulhaus Kopfholz",
			Organization:    "Adliswil",
			Email:           "schulverwaltung@adliswil.ch",
			ConfidenceScore: 0.9,
		},
	}

	subject, body := buildCaseEscalationDraft(detail, targets, "", "")

	if !strings.Contains(subject, "CleanApp-Meldung") {
		t.Fatalf("expected localized German subject, got %q", subject)
	}
	if !strings.Contains(body, "Guten Tag") {
		t.Fatalf("expected German greeting in body, got %q", body)
	}
	if !strings.Contains(body, "Schweregrad: 100%") {
		t.Fatalf("expected severity line in body, got %q", body)
	}
}

func TestNormalizeManualCCEmailsDedupesAndExcludesPrimaryTargets(t *testing.T) {
	targets := []models.CaseEscalationTarget{
		{Email: "maintenance@adliswil.ch"},
	}

	got := normalizeManualCCEmails(
		[]string{
			"maintenance@adliswil.ch, safety@adliswil.ch",
			"ops@adliswil.ch; safety@adliswil.ch",
			"  mayor@adliswil.ch  ",
		},
		targets,
	)

	want := []string{
		"mayor@adliswil.ch",
		"ops@adliswil.ch",
		"safety@adliswil.ch",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d cc emails, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected cc[%d] = %q, got %q", i, want[i], got[i])
		}
	}
}
