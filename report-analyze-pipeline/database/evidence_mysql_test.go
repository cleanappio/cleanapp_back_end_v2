package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"github.com/go-sql-driver/mysql"
)

// Retained as an execution oracle for candidate eligibility and for reproducing
// the optimizer's source lookups before completed-evidence exclusion.
const legacyPendingEvidenceQuery = `SELECT /*+ MAX_EXECUTION_TIME(8000) */ r.seq,
 (SELECT COALESCE(MAX(brand_name), '') FROM report_analysis WHERE seq = r.seq)
 FROM reports r LEFT JOIN report_evidence ev ON ev.seq = r.seq
 WHERE r.seq > GREATEST(0, CAST((SELECT COALESCE(MAX(seq), 0) FROM reports) AS SIGNED) - 50000)
 AND COALESCE(ev.evidence_version, 0) = 0
 AND (SELECT 1 FROM report_analysis ra WHERE ra.seq = r.seq LIMIT 1) IS NOT NULL
 AND (r.ts >= UTC_TIMESTAMP() - INTERVAL 1 DAY
   OR EXISTS (SELECT 1 FROM wire_submissions_raw ws WHERE ws.report_seq = r.seq AND ws.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY)
   OR EXISTS (SELECT 1 FROM external_ingest_index ei WHERE ei.seq = r.seq AND ei.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY))
 ORDER BY r.seq DESC LIMIT ?`

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
	want := []PendingEvidence{{Seq: 10007, Brand: "openai"}, {Seq: 10004, Brand: "openai"}, {Seq: 10003, Brand: "claude"}, {Seq: 10002, Brand: "grok"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recent/cutoff/version/translation eligibility incorrect: %#v", got)
	}
	if old := pendingEvidenceFixtureQuery(t, db, legacyPendingEvidenceQuery, 50); !reflect.DeepEqual(got, old) {
		t.Fatal("materialized candidate eligibility differs from original query")
	}
	limited, err := store.GetPendingEvidenceReports(context.Background(), 2)
	if err != nil || !reflect.DeepEqual(limited, want[:2]) {
		t.Fatalf("bounded newest-first batch incorrect: %#v err=%v", limited, err)
	}
	// Old publication dates qualify through recent immutable server ingestion;
	// old sequence values, old imports, completed rows, and unanalyzed rows do not.
}

func pendingEvidenceFixtureQuery(t *testing.T, db *sql.DB, query string, limit int) []PendingEvidence {
	t.Helper()
	rows, err := db.Query(query, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	items := []PendingEvidence{}
	for rows.Next() {
		var item PendingEvidence
		if err := rows.Scan(&item.Seq, &item.Brand); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestPendingEvidenceMySQLCompletedRowsAvoidSourceLookups(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_TEST_DSN to a dedicated local MySQL database ending _evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture only permits dedicated local databases ending _evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open local fixture database")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version string
	var performanceSchema int
	if err := db.QueryRow(`SELECT VERSION(),@@performance_schema`).Scan(&version, &performanceSchema); err != nil || !strings.HasPrefix(version, "8.0.44") || performanceSchema != 1 {
		t.Fatal("lookup proof requires actual MySQL 8.0.44 with performance_schema enabled")
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, statement := range []string{
		`CREATE TABLE reports (seq INT UNSIGNED PRIMARY KEY, ts DATETIME)`,
		`CREATE TABLE report_analysis (seq INT, language VARCHAR(2), brand_name VARCHAR(64), INDEX seq_index(seq))`,
		`CREATE TABLE wire_submissions_raw (report_seq INT, created_at DATETIME, INDEX report_seq_index(report_seq))`,
		`CREATE TABLE external_ingest_index (seq INT, created_at DATETIME, INDEX seq_index(seq))`,
	} {
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
	for seq := 60000; seq < 60200; seq++ {
		for _, query := range []string{
			`INSERT INTO reports VALUES (?, '2000-01-01')`,
			`INSERT INTO report_analysis VALUES (?, 'en', 'grok')`,
			`INSERT INTO wire_submissions_raw VALUES (?, '2000-01-01')`,
			`INSERT INTO external_ingest_index VALUES (?, '2000-01-01')`,
		} {
			if _, err := db.Exec(query, seq); err != nil {
				t.Fatal(err)
			}
		}
		if err := evidence.Save(context.Background(), db, seq, evidence.Unknown("Completed original assessment"), evidence.Version); err != nil {
			t.Fatal(err)
		}
	}
	fetches := func() map[string]uint64 {
		t.Helper()
		rows, err := db.Query(`SELECT OBJECT_NAME,COUNT_FETCH FROM performance_schema.table_io_waits_summary_by_table WHERE OBJECT_SCHEMA=DATABASE() AND OBJECT_NAME IN ('wire_submissions_raw','external_ingest_index')`)
		if err != nil {
			t.Fatal("fixture cannot read source lookup counters")
		}
		defer rows.Close()
		counts := map[string]uint64{}
		for rows.Next() {
			var name string
			var count uint64
			if err := rows.Scan(&name, &count); err != nil {
				t.Fatal(err)
			}
			counts[name] = count
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return counts
	}
	before := fetches()
	if rows := pendingEvidenceFixtureQuery(t, db, legacyPendingEvidenceQuery, 50); len(rows) != 0 {
		t.Fatal("completed fixture unexpectedly eligible in original query")
	}
	afterOld := fetches()
	if rows := pendingEvidenceFixtureQuery(t, db, pendingEvidenceQuery, 50); len(rows) != 0 {
		t.Fatal("completed fixture unexpectedly eligible in gated query")
	}
	afterNew := fetches()
	for _, name := range []string{"wire_submissions_raw", "external_ingest_index"} {
		oldReads := afterOld[name] - before[name]
		newReads := afterNew[name] - afterOld[name]
		if oldReads == 0 || newReads != 0 {
			t.Fatalf("completed-source lookup proof failed for %s: legacy=%d gated=%d", name, oldReads, newReads)
		}
		t.Logf("%s completed-row fetches legacy=%d gated=%d", name, oldReads, newReads)
	}
	var encoded string
	if err := db.QueryRow(`EXPLAIN FORMAT=JSON `+pendingEvidenceQuery, 50).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(encoded), &plan); err != nil {
		t.Fatal("invalid fixture query plan")
	}
	materialized, covered := false, false
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if node["table_name"] == "c" && node["materialized_from_subquery"] != nil {
				materialized = true
			}
			if node["table_name"] == "candidate" && node["key"] == "PRIMARY" && node["using_index"] == true {
				covered = true
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(plan)
	if !materialized || !covered {
		t.Fatal("eligible IDs must be materialized from the covered primary-key scan")
	}
	// Missing evidence and transient version0 remain retryable after genuine
	// recent server ingestion, even when original publication timestamps are old.
	for _, query := range []string{
		`INSERT INTO reports VALUES (60200,'2000-01-01'),(60201,'2000-01-01'),(60202,UTC_TIMESTAMP())`,
		`INSERT INTO report_analysis VALUES (60200,'en','claude'),(60201,'en','openai')`,
		`INSERT INTO wire_submissions_raw VALUES (60200,UTC_TIMESTAMP()),(60202,UTC_TIMESTAMP())`,
		`INSERT INTO external_ingest_index VALUES (60201,UTC_TIMESTAMP())`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := evidence.Save(context.Background(), db, 60201, evidence.Unknown("Transient provider failure"), 0); err != nil {
		t.Fatal(err)
	}
	newRows := pendingEvidenceFixtureQuery(t, db, pendingEvidenceQuery, 50)
	oldRows := pendingEvidenceFixtureQuery(t, db, legacyPendingEvidenceQuery, 50)
	want := []PendingEvidence{{Seq: 60201, Brand: "openai"}, {Seq: 60200, Brand: "claude"}}
	if !reflect.DeepEqual(newRows, oldRows) || !reflect.DeepEqual(newRows, want) {
		t.Fatal("missing/version0 ingestion parity changed")
	}
	if rows := pendingEvidenceFixtureQuery(t, db, pendingEvidenceQuery, 1); !reflect.DeepEqual(rows, want[:1]) {
		t.Fatal("newest-first batch limit changed")
	}
	t.Logf("materialized=%t covered_primary=%t exact_retry_parity=%d", materialized, covered, len(newRows))
}
