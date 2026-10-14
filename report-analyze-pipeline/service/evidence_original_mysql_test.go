package service

import (
	"cleanapp-common/evidence"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// This opt-in fixture uses a dedicated disposable database and actual MySQL
// 8.0.44 to reproduce sorting a retained JSON larger than the sort buffer.
func TestLargeWireOriginalAvoidsPayloadSortMySQL(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_ORIGINAL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated original resolver fixture not configured")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || parsed.DBName != "original_resolver_evidence_test" {
		t.Fatal("fixture requires separate original_resolver_evidence_test database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("fixture connection configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `SET SESSION sort_buffer_size = 262144`); err != nil {
		t.Fatal("fixture session configuration failed")
	}
	var version string
	if err := db.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&version); err != nil || !strings.HasPrefix(version, "8.0.44") {
		t.Fatal("fixture requires actual MySQL 8.0.44")
	}
	tables := []string{"wire_submissions_raw", "report_raw", "external_ingest_index", "reports"}
	cleanup := func() {
		for _, table := range tables {
			if _, err := db.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
				t.Error("fixture table cleanup failed")
			}
		}
	}
	cleanup()
	defer cleanup()
	for _, query := range []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY, description TEXT, image LONGBLOB)`,
		`CREATE TABLE external_ingest_index (seq INT, source VARCHAR(64), external_id VARCHAR(512), INDEX idx_seq(seq))`,
		`CREATE TABLE report_raw (report_seq INT PRIMARY KEY, source_id VARCHAR(512), source_type VARCHAR(64))`,
		`CREATE TABLE wire_submissions_raw (submission_id VARCHAR(80) PRIMARY KEY, report_seq INT, report_json JSON, actor_kind VARCHAR(16), channel VARCHAR(16), created_at TIMESTAMP NOT NULL, INDEX fk_wire_submission_report(report_seq))`,
		`INSERT INTO reports (seq,description) VALUES (1,'Generated display summary')`,
		`INSERT INTO report_raw (report_seq,source_id,source_type) VALUES (1,'human_mobile_fixture','sensor')`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal("fixture setup failed")
		}
	}
	annotation := "After clicking Save, the application closes."
	makePayload := func(description string, redacted bool) []byte {
		payload, err := json.Marshal(map[string]any{"problem_type": "citizen_submission", "description": description, "privacy": map[string]bool{"requires_redaction": redacted}, "image_base64": strings.Repeat("A", 512000)})
		if err != nil {
			t.Fatal("synthetic payload construction failed")
		}
		return payload
	}
	for _, row := range []struct {
		id, actor, channel, created string
		payload                     []byte
	}{
		{"older-z", "machine", "wire", "2026-10-13 10:00:00", makePayload("Earlier generated annotation", false)},
		{"newest-a", "human", "mobile", "2026-10-14 10:00:00", makePayload(annotation, true)},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wire_submissions_raw (submission_id,report_seq,report_json,actor_kind,channel,created_at) VALUES (?,1,?,?,?,?)`, row.id, row.payload, row.actor, row.channel, row.created); err != nil {
			t.Fatal("synthetic Wire insert failed")
		}
	}
	// Reproduce the production failure on the same index layout and buffer size.
	var payload []byte
	var actor, channel string
	err = db.QueryRowContext(ctx, `SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`, 1).Scan(&payload, &actor, &channel)
	var databaseError *mysql.MySQLError
	if !errors.As(err, &databaseError) || databaseError.Number != 1038 {
		t.Fatal("legacy wide Wire sort did not reproduce MySQL 1038")
	}
	got, err := evidence.LoadOriginal(ctx, db, 1)
	if err != nil {
		t.Fatal("large Wire original lookup failed")
	}
	if got.Text != annotation || got.SourceType != "human_submission" || !got.RequiresRedaction || got.SourceAuthor != "" {
		t.Fatal("newest original source/provenance/privacy was not preserved")
	}
	t.Log("legacy 1038 reproduced; newest 512k Wire original resolved with privacy preserved")
}
