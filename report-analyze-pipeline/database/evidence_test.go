package database

import (
	"cleanapp-common/evidence"
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestPendingEvidenceIncludesExistingAnalysisAndUsesBoundedIngestionIndex(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := &Database{db: sqlDB}
	mock.ExpectQuery(`SELECT r.seq, .*FROM reports r FORCE INDEX \(idx_reports_evidence_ingested\).*EXISTS \(SELECT 1 FROM report_analysis ra WHERE ra.seq = r.seq AND ra.evidence_version = 0\).*LIMIT`).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(99, "grok"))
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
	query := `UPDATE report_analysis SET report_kind = ?, evidence_excerpt = ?, reported_impact = ?, classification_reason = ?, entity_relationship = ?, evidence_version = ? WHERE seq = ? AND COALESCE(evidence_version,0) < 1`
	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs(fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, evidence.Version, 99).WillReturnResult(sqlmock.NewResult(0, 3))
	if err := db.SaveEvidence(context.Background(), 99, fields); err != nil {
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
	mock.ExpectExec(`INSERT INTO report_analysis`).WithArgs(44, "Gemini", "", []byte(nil), "", "", "grok", "Grok", float64(0), float64(0), float64(0), float64(0), "", "en", true, "digital", "", "", fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, 0).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := db.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}
	// The production worker selects existing analysis whose failed evidence is
	// still version0, rather than requiring the entire report to be unanalyzed.
	mock.ExpectQuery(`SELECT r.seq, .*ra.evidence_version = 0.*LIMIT`).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(44, "grok"))
	pending, err := db.GetPendingEvidenceReports(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].Seq != 44 {
		t.Fatalf("not retryable: %#v err=%v", pending, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
