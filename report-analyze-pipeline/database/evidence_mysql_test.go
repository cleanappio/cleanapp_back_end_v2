package database

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"github.com/go-sql-driver/mysql"
)

// Only a dedicated local fixture database is allowed. This checks actual MySQL
// candidate semantics without changing any production tables or model calls.
func TestPendingEvidenceMySQLBoundedRecentImports(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_TEST_DSN to a dedicated local MySQL database ending _evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid local fixture DSN")
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture only permits dedicated local databases ending _evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open local fixture database")
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE reports (seq INT UNSIGNED PRIMARY KEY, ts DATETIME)`,
		`CREATE TABLE report_analysis (seq INT, language VARCHAR(2), brand_name VARCHAR(64), INDEX seq_index(seq))`,
		`CREATE TABLE wire_submissions_raw (report_seq INT, created_at DATETIME, INDEX report_seq_index(report_seq))`,
		`CREATE TABLE external_ingest_index (seq INT, created_at DATETIME, INDEX seq_index(seq))`,
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal("fixture requires an empty local test database")
		}
		created = append(created, strings.Fields(statement)[2])
	}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_evidence'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("fixture refuses a pre-existing evidence table")
	}
	created = append(created, "report_evidence")
	if err := evidence.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := &Database{db: db}
	if got, err := store.GetPendingEvidenceReports(context.Background(), 50); err != nil || len(got) != 0 {
		t.Fatalf("empty unsigned table failed: %#v err=%v", got, err)
	}
	if _, err := db.Exec(`INSERT INTO reports VALUES (1,UTC_TIMESTAMP())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO report_analysis VALUES (1,'en','grok')`); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetPendingEvidenceReports(context.Background(), 50); err != nil || len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("small unsigned sequence underflow: %#v err=%v", got, err)
	}
	fixtures := []string{
		`INSERT INTO reports VALUES (10002,'2000-01-01'),(10003,'2000-01-01'),(10004,UTC_TIMESTAMP()),(10005,'2000-01-01'),(10006,UTC_TIMESTAMP()),(10007,UTC_TIMESTAMP()),(10008,UTC_TIMESTAMP()),(60001,'2000-01-01')`,
		`INSERT INTO report_analysis VALUES (10002,'en','grok'),(10002,'de','grok'),(10003,'en','claude'),(10004,'en','openai'),(10005,'en','grok'),(10006,'en','grok'),(10007,'en','openai')`,
		`INSERT INTO wire_submissions_raw VALUES (1,UTC_TIMESTAMP()),(10002,UTC_TIMESTAMP()),(10005,'2000-01-01')`,
		`INSERT INTO external_ingest_index VALUES (10003,UTC_TIMESTAMP()),(10005,'2000-01-01')`,
	}
	for _, statement := range fixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := evidence.Save(context.Background(), db, 10006, evidence.Unknown("Completed insufficient evidence"), evidence.Version); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Save(context.Background(), db, 10007, evidence.Unknown("Transient retry"), 0); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPendingEvidenceReports(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []PendingEvidence{{Seq: 10002, Brand: "grok"}, {Seq: 10003, Brand: "claude"}, {Seq: 10004, Brand: "openai"}, {Seq: 10007, Brand: "openai"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recent/cutoff/version/translation eligibility incorrect: %#v", got)
	}
	limited, err := store.GetPendingEvidenceReports(context.Background(), 2)
	if err != nil || !reflect.DeepEqual(limited, want[:2]) {
		t.Fatalf("bounded ascending batch incorrect: %#v err=%v", limited, err)
	}
	// Old publication dates qualify through recent immutable server ingestion;
	// old sequence values, old imports, completed rows, and unanalyzed rows do not.
}
