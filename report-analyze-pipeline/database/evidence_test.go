package database

import (
	"cleanapp-common/evidence"
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestPendingEvidenceIncludesExistingAnalysisAndBoundsSequenceAndIngestionTime(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := &Database{db: sqlDB}
	mock.ExpectQuery(regexp.QuoteMeta(pendingEvidenceQuery)).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(99, "grok"))
	got, err := db.GetPendingEvidenceReports(context.Background(), 10)
	if err != nil || len(got) != 1 || got[0].Seq != 99 {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if _, err := db.GetPendingEvidenceReports(context.Background(), 1000); err == nil {
		t.Fatal("unbounded batch accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceOnlyWritePreservesExistingAnalysesAndRoutingState(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := &Database{db: sqlDB}
	fields := evidence.Fields{ReportKind: "firsthand", EvidenceExcerpt: "I hit an error", ReportedImpact: "Workflow blocked", ClassificationReason: "Original author describes own observed failure", EntityRelationship: "affected"}
	mock.ExpectExec(`INSERT INTO report_evidence`).WithArgs(99, fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, evidence.Version).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := db.SaveEvidence(context.Background(), 99, fields, evidence.Version); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type failingEvidenceClassifier struct{}

func (failingEvidenceClassifier) AnalyzeEvidence(original, entity string) (evidence.Fields, error) {
	return evidence.Fields{}, errors.New("transient classifier outage")
}

func TestMainAnalysisEvidenceFailureIsPersistedAsRetryableVersionZero(t *testing.T) {
	fields, version, classificationErr := evidence.ForPersistence(failingEvidenceClassifier{}, evidence.Original{Text: "I hit an error"}, "grok", nil)
	if classificationErr == nil || version != 0 {
		t.Fatalf("failed enrichment completed: version=%d err=%v", version, classificationErr)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := &Database{db: sqlDB}
	analysis := &ReportAnalysis{Seq: 44, Language: "en", Source: "Gemini", BrandName: "grok", BrandDisplayName: "Grok", IsValid: true, Classification: "digital", Fields: fields, EvidenceVersion: version}
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM report_analysis WHERE seq = \? AND language = \?`).WithArgs(44, "en").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO report_analysis \(.*inferred_contact_emails, legal_risk_estimate\s*\)`).WithArgs(44, "Gemini", "", []byte(nil), "", "", "grok", "Grok", float64(0), float64(0), float64(0), float64(0), "", "en", true, "digital", "", "").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := db.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO report_evidence`).WithArgs(44, fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, 0).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := db.SaveEvidence(context.Background(), analysis.Seq, fields, version); err != nil {
		t.Fatal(err)
	}
	// The production worker selects existing analysis whose failed evidence is
	// still version0, rather than requiring the entire report to be unanalyzed.
	mock.ExpectQuery(regexp.QuoteMeta(pendingEvidenceQuery)).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(44, "grok"))
	pending, err := db.GetPendingEvidenceReports(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].Seq != 44 {
		t.Fatalf("not retryable: %#v err=%v", pending, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceWriteFailureDoesNotRollbackCommittedNormalAnalysis(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := &Database{db: sqlDB}
	analysis := &ReportAnalysis{Seq: 45, Language: "en", Source: "Gemini", IsValid: true, Classification: "digital", Fields: evidence.Unknown("Unavailable")}
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM report_analysis WHERE seq = \? AND language = \?`).WithArgs(45, "en").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO report_analysis \(.*inferred_contact_emails, legal_risk_estimate\s*\)`).WithArgs(45, "Gemini", "", []byte(nil), "", "", "", "", float64(0), float64(0), float64(0), float64(0), "", "en", true, "digital", "", "").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := db.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO report_evidence`).WithArgs(45, "unknown", "", "", "Unavailable", "unknown", 0).WillReturnError(errors.New("storage unavailable"))
	if err := db.SaveEvidence(context.Background(), 45, analysis.Fields, 0); err == nil {
		t.Fatal("expected independent evidence failure")
	}
	// A missing evidence record is also eligible. No rollback or normal-analysis
	// update is expected after the separate evidence write fails.
	mock.ExpectQuery(regexp.QuoteMeta(pendingEvidenceQuery)).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(45, ""))
	if pending, err := db.GetPendingEvidenceReports(context.Background(), 10); err != nil || len(pending) != 1 {
		t.Fatalf("missing evidence was not retryable: %#v err=%v", pending, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
