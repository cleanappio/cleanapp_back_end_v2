package stubllm

import (
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"report-analyze-pipeline/parser"
)

func TestOutcomeFixtureEvidenceUsesLiteralRetainedSourceAndExplicitMarkers(t *testing.T) {
	for _, tc := range []struct {
		text, kind string
	}{
		{"The checkout button failed when I clicked it.", "firsthand"},
		{"[outcome:unknown] I have no observation context.", "unknown"},
		{"[outcome:coverage] A source describes the checkout failure.", "coverage"},
		{"", "unknown"},
	} {
		fields, version, err := evidence.ForPersistence(NewClient(), evidence.Original{Text: tc.text}, "Fixture", nil)
		if err != nil || version != evidence.Version || fields.ReportKind != tc.kind {
			t.Fatalf("fixture kind=%q version=%d err=%v", fields.ReportKind, version, err)
		}
		if tc.kind != "unknown" && (fields.EntityRelationship != "affected" || fields.EvidenceExcerpt == "" || !strings.Contains(tc.text, fields.EvidenceExcerpt)) {
			t.Fatal("fixture invented evidence or lost the original excerpt")
		}
	}
}

func TestOutcomeFixtureRejectMarkerChangesOnlyValidity(t *testing.T) {
	client := NewClient()
	for _, text := range []string{"Observed fixture problem", "[outcome:rejected] Not a reportable fixture problem"} {
		first, err := client.AnalyzeImage(nil, text)
		if err != nil {
			t.Fatal(err)
		}
		second, err := client.AnalyzeImage(nil, text)
		if err != nil || first != second {
			t.Fatal("fixture analysis is not deterministic")
		}
		analysis, err := parser.ParseAnalysis(first)
		if err != nil || analysis.IsValid == strings.Contains(text, "[outcome:rejected]") {
			t.Fatalf("fixture validity did not honor its explicit marker: %v", err)
		}
	}
}
