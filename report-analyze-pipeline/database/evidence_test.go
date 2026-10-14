package database

import (
	"cleanapp-common/evidence"
	"context"
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
