package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

var storedAgentAnalysisColumns = []string{"seq", "source", "analysis_text", "title", "description",
	"brand_name", "brand_display_name", "litter_probability", "hazard_probability", "digital_bug_probability",
	"severity_level", "summary", "language", "is_valid", "classification", "inferred_contact_emails",
	"legal_risk_estimate", "report_kind", "evidence_excerpt", "reported_impact", "classification_reason",
	"entity_relationship", "evidence_version"}

func storedAgentAnalysisRow(language, title string, evidenceVersion int) []driver.Value {
	return []driver.Value{42, "fixture-provider", "retained provider response", title, "Retained description",
		"fixture", "Fixture", 0.0, 0.0, 0.9, 0.3, "Retained summary", language, true, "digital", "", "",
		"firsthand", "The checkout button failed", "", "Original describes an observed failure", "affected", evidenceVersion}
}

func TestStoredAgentAnalysisReusesEnglishTranslationsAndPersistedEvidence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := sqlmock.NewRows(storedAgentAnalysisColumns).
		AddRow(storedAgentAnalysisRow("en", "Latest English", 1)...).
		AddRow(storedAgentAnalysisRow("en", "Older English", 1)...).
		AddRow(storedAgentAnalysisRow("de", "Stored translation", 1)...)
	mock.ExpectQuery(`FROM report_analysis ra LEFT JOIN report_evidence ev ON ev.seq=ra.seq`).
		WithArgs(42).WillReturnRows(rows)
	got, err := (&Database{db: db}).LoadAgentReportAnalyses(context.Background(), 42)
	if err != nil || len(got) != 2 {
		t.Fatalf("stored analysis lookup: rows=%d err=%v", len(got), err)
	}
	if got[0].Title != "Latest English" || got[0].Language != "en" || got[1].Language != "de" ||
		got[0].EvidenceVersion != 1 || got[0].ReportKind != "firsthand" || got[1].EvidenceExcerpt != got[0].EvidenceExcerpt {
		t.Fatal("stored analysis lost language, source evidence, or preferred row")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStoredAgentAnalysisNeedsEnglishButMissingEvidenceDoesNotDiscardProviderWork(t *testing.T) {
	for _, language := range []string{"de", "en"} {
		t.Run(language, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			row := storedAgentAnalysisRow(language, "Saved provider result", 0)
			row[17], row[18], row[20], row[21] = "unknown", "", "", "unknown"
			mock.ExpectQuery(`FROM report_analysis ra LEFT JOIN report_evidence ev ON ev.seq=ra.seq`).
				WithArgs(42).WillReturnRows(sqlmock.NewRows(storedAgentAnalysisColumns).AddRow(row...))
			got, err := (&Database{db: db}).LoadAgentReportAnalyses(context.Background(), 42)
			if err != nil {
				t.Fatal(err)
			}
			if language == "en" {
				if len(got) != 1 || got[0].EvidenceVersion != 0 || got[0].AnalysisText == "" {
					t.Fatal("lost saved provider response merely because evidence is incomplete")
				}
			} else if len(got) != 0 {
				t.Fatal("a translation alone was mistaken for completed English analysis")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoredAgentAnalysisLookupFailureCannotAuthorizeAnotherModelCall(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`FROM report_analysis ra LEFT JOIN report_evidence ev ON ev.seq=ra.seq`).
		WithArgs(42).WillReturnError(errors.New("fixture database unavailable"))
	got, err := (&Database{db: db}).LoadAgentReportAnalyses(context.Background(), 42)
	if err == nil || got != nil {
		t.Fatal("unavailable lookup was treated as no previous provider work")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
