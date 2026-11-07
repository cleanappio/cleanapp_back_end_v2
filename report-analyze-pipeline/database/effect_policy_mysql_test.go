package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"cleanapp-common/effectpolicy"
	"github.com/go-sql-driver/mysql"
)

func TestHistoricalEvidenceSelectionMySQL(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_TEST_DSN to an isolated AWS MySQL fixture")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture requires a dedicated local database ending _evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, query := range []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY, ts DATETIME)`,
		`CREATE TABLE report_evidence (seq INT PRIMARY KEY, evidence_version INT)`,
		`CREATE TABLE report_analysis (seq INT PRIMARY KEY, brand_name VARCHAR(64))`,
		`CREATE TABLE wire_submissions_raw (report_seq INT, created_at DATETIME, INDEX(report_seq))`,
		`CREATE TABLE external_ingest_index (seq INT PRIMARY KEY, created_at DATETIME)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal("fixture requires an empty isolated database", err)
		}
		created = append(created, strings.Fields(query)[2])
	}
	if err := effectpolicy.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	created = append(created, "report_side_effect_policy")
	for _, query := range []string{
		`INSERT INTO reports VALUES (1,'2023-07-04'),(2,UTC_TIMESTAMP())`,
		`INSERT INTO report_analysis VALUES (1,''),(2,'')`,
		`INSERT INTO external_ingest_index VALUES (1,UTC_TIMESTAMP())`,
		`INSERT INTO report_side_effect_policy (report_seq,source_namespace,reason) VALUES (1,'dataunion-0.1.0','historical import')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	store := &Database{db: db}
	rows, err := store.GetPendingEvidenceChunk(context.Background(), 0, 2, 50)
	if err != nil || len(rows) != 1 || rows[0].Seq != 2 {
		t.Fatalf("expected only ordinary report, rows=%#v err=%v", rows, err)
	}
	if _, err := db.Exec(`DROP TABLE report_side_effect_policy`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPendingEvidenceChunk(context.Background(), 0, 2, 50); err == nil {
		t.Fatal("missing policy table did not fail closed")
	}
}
