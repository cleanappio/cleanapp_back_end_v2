package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cleanapp-common/eiiheader"
	"cleanapp-common/reportmeta"
	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

// Reproduces historical rows predating all three projection migrations. The
// guarded disposable fixture never uses application credentials or production.
func TestBrandProjectionRepairMySQLFixture(t *testing.T) {
	dsn := os.Getenv("BRAND_REPAIR_TEST_DSN")
	if dsn == "" {
		t.Skip("set BRAND_REPAIR_TEST_DSN for the dedicated local fixture")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "brand_repair_evidence_test" {
		t.Fatal("fixture only permits dedicated local brand_repair_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("repair fixture refuses an existing table")
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE IF EXISTS " + created[i])
		}
	}()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`CREATE TABLE reports(seq INT PRIMARY KEY,id VARCHAR(255) NOT NULL,public_id VARCHAR(32),ts TIMESTAMP NULL,latitude FLOAT NOT NULL,longitude FLOAT NOT NULL,image LONGBLOB,description TEXT) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_analysis(seq INT,brand_name VARCHAR(255),is_valid BOOL,classification VARCHAR(32),language VARCHAR(2),created_at DATETIME,severity_level FLOAT,KEY idx_ra_brand_valid_class_core(brand_name,is_valid,classification,seq,language,created_at,severity_level)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE external_ingest_index(seq INT NOT NULL,source VARCHAR(64) NOT NULL,external_id VARCHAR(255) NOT NULL,source_url VARCHAR(500),source_timestamp DATETIME,created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(source,external_id,seq),KEY idx_seq(seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE wire_submissions_raw(submission_id VARCHAR(64) PRIMARY KEY,report_seq INT,observed_at TIMESTAMP NULL,submitted_at TIMESTAMP NOT NULL,actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',channel VARCHAR(32) NOT NULL DEFAULT 'wire',report_json JSON,provenance_json JSON,extensions_json JSON,KEY fixture_seq(report_seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE notification_queue(seq INT PRIMARY KEY,state VARCHAR(32))`,
	} {
		exec(ddl)
		created = append(created, strings.Split(strings.Fields(ddl)[2], "(")[0])
	}
	// More than one maximum-sized batch and a duplicate translation exercise
	// keyset paging; unrelated brand, invalid, physical and upper-bound rows
	// must remain untouched even though they predate the same migrations.
	for seq := 1; seq <= 206; seq++ {
		exec(`INSERT INTO reports VALUES(?,?,?,'2026-01-01',47.376887,8.541694,?,?)`, seq, fmt.Sprint("legacy_", seq), fmt.Sprint("r_", seq), []byte("retained image"), "verbatim original")
		exec(`INSERT INTO report_analysis VALUES(?,'microsoft',TRUE,'digital','en','2026-01-01',0.5)`, seq)
	}
	exec(`INSERT INTO report_analysis VALUES(10,'microsoft',TRUE,'digital','fr','2026-01-01',0.5)`)
	for _, row := range []struct {
		seq   int
		brand string
		valid bool
		class string
	}{{300, "google", true, "digital"}, {301, "microsoft", false, "digital"}, {302, "microsoft", true, "physical"}} {
		exec(`INSERT INTO reports VALUES(?,?,?,'2026-01-01',0,0,NULL,'other retained original')`, row.seq, fmt.Sprint("other_", row.seq), fmt.Sprint("r_", row.seq))
		exec(`INSERT INTO report_analysis VALUES(?,?,?,?,'en','2026-01-01',0.5)`, row.seq, row.brand, row.valid, row.class)
	}
	exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url,source_timestamp) VALUES(1,'github_issue','one','https://example.test/one','2026-01-01'),(205,'github_issue','last','https://example.test/last','2026-01-01'),(300,'github_issue','unrelated','https://example.test/unrelated','2026-01-01')`)
	exec(`INSERT INTO wire_submissions_raw VALUES('original',1,'2026-01-01','2026-10-14','human','web','{"digital_context":{"metadata":{"author_did":1}},"privacy":{"contains_pii":false}}','{"generation_method":"native"}',NULL),('unrelated',300,NULL,'2026-10-14','machine','wire','{}','{}',NULL)`)
	exec(`INSERT INTO notification_queue VALUES(1,'pending')`)
	if verifyReady(context.Background(), db) == nil {
		t.Fatal("repair accepted absent migrations")
	}
	for _, migration := range []struct {
		table string
		up    func(context.Context, *sql.DB) error
	}{{"report_metadata_projection", reportmeta.Migrate}, {"external_ingest_header_projection", eiiheader.Migrate}, {"wire_evidence_projection", wiremeta.Migrate}} {
		created = append(created, migration.table)
		if err := migration.up(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyReady(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	s := sqlStore{db}
	c := config{Mode: "audit", Brand: "microsoft", ThroughSeq: 205, BatchSize: 100, Timeout: 10 * time.Second}
	counts, err := s.audit(context.Background(), []int{1, 205})
	if err != nil || counts.MissingReports != 2 || counts.MissingExternal != 2 || counts.MissingWire != 1 {
		t.Fatalf("did not reproduce historical cold projections: %+v err=%v", counts, err)
	}
	if execute(context.Background(), c, s, func(event) {}) == nil {
		t.Fatal("cold historical cohort incorrectly passed readiness")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection`).Scan(&count); err != nil || count != 0 {
		t.Fatal("audit changed historical projections")
	}
	c.Mode, c.MaxBatches = "apply", 1
	var last event
	if err := execute(context.Background(), c, s, func(e event) { last = e }); err != nil || last.Stage != "paused" || last.AfterSeq != 100 {
		t.Fatalf("bounded repair did not pause with exact checkpoint: %+v err=%v", last, err)
	}
	c.AfterSeq, c.MaxBatches = last.AfterSeq, 0
	if err := execute(context.Background(), c, s, func(e event) { last = e }); err != nil || last.Stage != "complete" || last.Counts.Reports != 205 || !last.Counts.complete() {
		t.Fatalf("resumed full-cohort audit failed: %+v err=%v", last, err)
	}
	// Replaying a checkpoint repairs/retains identical derived state. Raw
	// originals, privacy JSON, pending notifications and parent schema survive.
	if err := execute(context.Background(), c, s, func(event) {}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection m JOIN reports r ON r.seq=m.seq WHERE BINARY m.id=BINARY r.id AND m.public_id=r.public_id AND m.ts=r.ts AND m.latitude=r.latitude AND m.longitude=r.longitude AND m.has_original_media=(LENGTH(r.image)>0)`).Scan(&count); err != nil || count != 205 {
		t.Fatalf("bounded scalar projection differs from raw data: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reports WHERE description='verbatim original' AND image=CAST('retained image' AS BINARY)`).Scan(&count); err != nil || count != 206 {
		t.Fatal("repair changed a retained original")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection WHERE seq IN (206,300,301,302)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("repair escaped brand/validity/classification/sequence bounds")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_submissions_raw WHERE submission_id='original' AND JSON_TYPE(JSON_EXTRACT(report_json,'$.digital_context.metadata.author_did'))='INTEGER' AND JSON_TYPE(JSON_EXTRACT(report_json,'$.privacy.contains_pii'))='BOOLEAN' AND JSON_UNQUOTE(JSON_EXTRACT(report_json,'$.privacy.contains_pii'))='false'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("repair rewrote Wire content or JSON types")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_queue WHERE seq=1 AND state='pending'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("repair altered a pending notification")
	}
	// A presence-only readiness check would miss partial historical trigger
	// corruption. Require exact scalar and typed JSON fidelity before complete.
	exec(`UPDATE report_metadata_projection SET id='stale' WHERE seq=1`)
	exec(`UPDATE external_ingest_header_projection SET source_url='https://example.test/stale' WHERE seq=1`)
	exec(`UPDATE wire_evidence_projection SET author_did=CAST('2.0' AS JSON) WHERE submission_id='original'`)
	exec(`UPDATE wire_evidence_projection SET author_did=CAST('1.0' AS JSON) WHERE submission_id='original'`)
	counts, err = s.audit(context.Background(), []int{1})
	if err != nil || counts.StaleReports != 1 || counts.StaleExternal != 1 || counts.StaleWire != 1 || counts.complete() {
		t.Fatalf("fidelity audit accepted stale scalar/typed JSON values: %+v err=%v", counts, err)
	}
	c.Mode = "audit"
	if execute(context.Background(), c, s, func(event) {}) == nil {
		t.Fatal("full audit accepted stale projections")
	}
	c.Mode, c.AfterSeq = "apply", 0
	if err := execute(context.Background(), c, s, func(event) {}); err != nil {
		t.Fatal("replay failed to reconcile stale projection values:", err)
	}
	exec(`UPDATE reports SET image=NULL,description='new committed original' WHERE seq=1`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection WHERE seq=1 AND has_original_media=FALSE`).Scan(&count); err != nil || count != 1 {
		t.Fatal("existing metadata trigger stopped maintaining repaired history")
	}
}
