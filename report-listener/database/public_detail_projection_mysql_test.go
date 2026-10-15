package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"report-listener/models"
)

// Fresh MySQL databases use 0900_ai_ci for reports, while the metadata
// projection deliberately retains legacy unicode_ci. Derived source identities
// need an explicit binary collation before public equality/URL rules use them.
func testPublicDetailMixedMetadataCollationsMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`ALTER TABLE reports MODIFY COLUMN id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '', MODIFY COLUMN public_id VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`,
		`INSERT INTO reports(seq,public_id,ts,id,latitude,longitude,image) VALUES(4601,'rpt_0000000000000000004601','2026-10-16','wire-legacy-fixture',47.36,8.55,'')`,
		`INSERT INTO report_analysis VALUES(4601,'en','','',TRUE,'physical','2026-10-16','Observed obstruction','AI summary',0.2)`,
		`INSERT INTO report_raw VALUES(4601,'web','legacy-source-fixture','legacy-fetcher-fixture','public')`,
		`INSERT INTO wire_submissions_raw(report_seq,report_json,provenance_json,extensions_json,observed_at,submitted_at,actor_kind,channel) VALUES(4601,'{"domain":"physical","description":"Observed obstruction"}','{"generation_method":"legacy_v1_bulk_ingest"}','{"observation_time_supplied":true}','2026-02-14','2026-10-16','machine','wire')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	report := &models.ReportWithAnalysis{Report: models.Report{Seq: 4601}, Analysis: []models.ReportAnalysis{{Title: "Observed obstruction"}}}
	if err := (&Database{db: db}).AttachPublicEvidence(context.Background(), []*models.ReportWithAnalysis{report}); err != nil {
		t.Fatalf("mixed metadata/legacy physical projection failed: %v", err)
	}
	if report.Evidence == nil || report.Evidence.ReportKind != "unknown" || report.Evidence.ObservedAt == nil || report.Analysis[0].Title != "Observed obstruction" {
		t.Fatal("legacy public detail lost availability or fabricated evidence classification")
	}
}

func TestPublicDetailDerivedMetadataCollationsAreExplicit(t *testing.T) {
	query, _ := evidenceCTE(EvidenceFilter{Kind: "all"}, "ra.seq IN (?)", []interface{}{"en", 1})
	for _, field := range []string{"id", "public_id"} {
		if !strings.Contains(query, "CASE WHEN m.seq IS NOT NULL THEN m."+field+" ELSE fallback."+field+" END COLLATE utf8mb4_bin AS "+field) {
			t.Fatalf("derived %s may inherit incompatible metadata/fallback collations", field)
		}
	}
}
