package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

// Exercise the actual listener's parent factories, not only a handwritten DDL.
func TestWireProjectionBootstrapMySQLFixture(t *testing.T) {
	dsn := os.Getenv("WIRE_BOOTSTRAP_TEST_DSN")
	if dsn == "" {
		t.Skip("set WIRE_BOOTSTRAP_TEST_DSN for the dedicated local fixture")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "wire_projection_bootstrap_evidence_test" {
		t.Fatal("fixture only permits dedicated local wire_projection_bootstrap_evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	names := []string{"reports", "fetchers", "external_ingest_index", "wire_submissions_raw", "wire_submission_receipts", "wire_agent_reputation_metrics", "wire_issue_clusters", "wire_reward_records", "wire_evidence_projection"}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("bootstrap fixture refuses an existing table")
	}
	defer func() {
		for i := len(names) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE IF EXISTS " + names[i])
		}
	}()
	if _, err := db.Exec(`CREATE TABLE reports(seq INT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := ensureFetcherTables(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := ensureCleanAppWireTables(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var table, before, after string
	if err := db.QueryRow(`SHOW CREATE TABLE wire_submissions_raw`).Scan(&table, &before); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := wiremeta.Migrate(context.Background(), db); err != nil {
			t.Fatalf("fresh Wire bootstrap migration: %v", err)
		}
		if err := ensureCleanAppWireTables(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`SHOW CREATE TABLE wire_submissions_raw`).Scan(&table, &after); err != nil || before != after {
		t.Fatal("projection or bootstrap retry changed the Wire parent")
	}
	var projected int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection`).Scan(&projected); err != nil || projected != 0 {
		t.Fatal("fresh migration unexpectedly backfilled source data")
	}
}
