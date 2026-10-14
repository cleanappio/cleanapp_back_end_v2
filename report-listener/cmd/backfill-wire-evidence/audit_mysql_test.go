package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

func TestWireAuditMySQLFixture(t *testing.T) {
	dsn := os.Getenv("WIRE_AUDIT_TEST_DSN")
	if dsn == "" {
		t.Skip("set WIRE_AUDIT_TEST_DSN for the dedicated local fixture")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "wire_audit_evidence_test" {
		t.Fatal("fixture only permits dedicated local wire_audit_evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("audit fixture refuses an existing table")
	}
	defer db.Exec(`DROP TABLE IF EXISTS reports`)
	defer db.Exec(`DROP TABLE IF EXISTS wire_submissions_raw`)
	defer db.Exec(`DROP TABLE IF EXISTS wire_evidence_projection`)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE reports(seq INT PRIMARY KEY)`)
	exec(`INSERT INTO reports VALUES(1),(2)`)
	exec(`CREATE TABLE wire_submissions_raw (submission_id VARCHAR(64) PRIMARY KEY,report_seq INT NULL,observed_at TIMESTAMP NULL,submitted_at TIMESTAMP NOT NULL,actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',channel VARCHAR(32) NOT NULL DEFAULT 'wire',report_json JSON,provenance_json JSON,extensions_json JSON,KEY fixture_seq(report_seq),CONSTRAINT fixture_report_fk FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	exec(`INSERT INTO wire_submissions_raw VALUES ('first',1,NULL,'2026-10-14','machine','wire','{"digital_context":{"metadata":{"source_url":null},"source_url":"https://example.test/source"},"privacy":{"contains_pii":false}}','{"generation_method":"import"}','{"observation_time_supplied":true}'),('second',2,'2026-10-01','2026-10-14','human','web','{"digital_context":{"metadata":{"author_did":1}}}','{}',NULL)`)
	if err := wiremeta.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	counts, err := auditBatch(context.Background(), db, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if counts.ParentRows != 2 || counts.HeaderRows != 0 || counts.MissingHeaders != 2 || counts.equal() {
		t.Fatal("audit accepted missing projected history")
	}
	if _, err := wiremeta.Backfill(context.Background(), db, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	// URI CASE expressions have a SQL string result. JSON-column assignment
	// parses that result; compare the same typed value when auditing a copy.
	node := wiremeta.NodeExpressions("ws")[2]
	var rawMismatch, typedMismatch int
	if err := db.QueryRow(`SELECT NOT(`+node+` <=> h.metadata_source_url),NOT(CAST(`+node+` AS JSON) <=> h.metadata_source_url) FROM wire_submissions_raw ws JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id WHERE ws.submission_id='first'`).Scan(&rawMismatch, &typedMismatch); err != nil {
		t.Fatal(err)
	}
	t.Logf("literal-null URI comparison: raw_mismatch=%d typed_mismatch=%d", rawMismatch, typedMismatch)
	if typedMismatch != 0 {
		t.Fatal("copied URI node differs after exact JSON typing")
	}

	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || !counts.equal() || counts.ParentRows != 2 {
		t.Fatalf("copied scalar audit differs: %+v err=%v", counts, err)
	}
	// JSON numeric comparison considers integer 1 and double 1.0 equal, but
	// their stored node type and JSON_UNQUOTE source semantics differ.
	// MySQL skips 1 -> 1.0 as a semantically equal JSON assignment. Change
	// the value first so the corruption really persists as a DOUBLE node.
	exec(`UPDATE wire_evidence_projection SET author_did=CAST('2.0' AS JSON) WHERE submission_id='second'`)
	exec(`UPDATE wire_evidence_projection SET author_did=CAST('1.0' AS JSON) WHERE submission_id='second'`)
	var originalNumericType, projectedNumericType string
	if err := db.QueryRow(`SELECT JSON_TYPE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_did')),JSON_TYPE(h.author_did) FROM wire_submissions_raw ws JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id WHERE ws.submission_id='second'`).Scan(&originalNumericType, &projectedNumericType); err != nil || originalNumericType != "INTEGER" || projectedNumericType != "DOUBLE" {
		t.Fatalf("numeric corruption did not persist: original=%s projected=%s err=%v", originalNumericType, projectedNumericType, err)
	}
	var numericEqual int
	if err := db.QueryRow(`SELECT JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_did') <=> h.author_did FROM wire_submissions_raw ws JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id WHERE ws.submission_id='second'`).Scan(&numericEqual); err != nil || numericEqual != 1 {
		t.Fatal("fixture did not reproduce JSON numeric type equality")
	}
	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || counts.StaleHeaders != 1 || counts.equal() {
		t.Fatalf("numeric source node audit: counts=%+v err=%v", counts, err)
	}
	if _, err := wiremeta.Backfill(context.Background(), db, []int{2}); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`SELECT JSON_TYPE(author_did) FROM wire_evidence_projection WHERE submission_id='second'`).Scan(&projectedNumericType); err != nil || projectedNumericType != "INTEGER" {
		t.Fatalf("snapshot repair retained a different numeric type: projected=%s err=%v", projectedNumericType, err)
	}
	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || !counts.equal() {
		t.Fatalf("snapshot repair failed immediate exact audit: counts=%+v err=%v", counts, err)
	}

	// Absent SQL NULL differs from a present literal JSON null; the numeric audit
	// must notice that corruption rather than silently treating both as missing.
	exec(`UPDATE wire_evidence_projection SET requires_redaction=CAST('null' AS JSON) WHERE submission_id='second'`)
	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || counts.StaleHeaders != 1 || counts.equal() {
		t.Fatal("audit conflated JSON null with absent SQL NULL")
	}
	if _, err := wiremeta.Backfill(context.Background(), db, []int{2}); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE wire_submissions_raw SET report_json=JSON_OBJECT('privacy',JSON_OBJECT('requires_redaction',TRUE)) WHERE submission_id='second'`)
	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || !counts.equal() {
		t.Fatal("transactional privacy update did not remain audit-consistent")
	}
	// MySQL report SET NULL bypasses the parent's UPDATE trigger. Retained
	// unlinked headers are ineligible via the original identity scan and must
	// be counted separately rather than incorrectly failing terminal readiness.
	exec(`DELETE FROM reports WHERE seq=2`)
	counts, err = auditBatch(context.Background(), db, []int{1, 2})
	if err != nil || !counts.equal() || counts.ParentRows != 1 || counts.HeaderRows != 1 || counts.IgnoredUnlinkedHeaders != 1 {
		t.Fatalf("legitimate report SETNULL failed terminal audit: %+v err=%v", counts, err)
	}

	exec(`UPDATE wire_evidence_projection SET report_seq=999 WHERE submission_id='first'`)
	counts, err = auditBatch(context.Background(), db, []int{1, 2, 999})
	if err != nil || counts.MissingHeaders != 1 || counts.MissingParents != 1 || counts.equal() {
		t.Fatal("audit accepted stale projected report link")
	}
}
