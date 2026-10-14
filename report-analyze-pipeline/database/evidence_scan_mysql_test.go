package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"github.com/go-sql-driver/mysql"
)

func TestEvidenceCursorMySQLBoundedScansAndFullWindowRecall(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_TEST_DSN to a dedicated local database ending _evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture only permits dedicated local databases ending _evidence_test")
	}
	cfg.InterpolateParams = true // expose candidate ROWS_EXAMINED to the fixture's own PFS connection
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open local fixture")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version string
	var pfs int
	if err := db.QueryRow(`SELECT VERSION(),@@performance_schema`).Scan(&version, &pfs); err != nil || !strings.HasPrefix(version, "8.0.44") || pfs != 1 {
		t.Fatal("bounded lookup proof requires actual MySQL8.0.44/PFS")
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, statement := range []string{
		`CREATE TABLE reports(seq INT UNSIGNED PRIMARY KEY,ts DATETIME)`,
		`CREATE TABLE report_analysis(seq INT,language VARCHAR(2),brand_name VARCHAR(64),INDEX seq_index(seq))`,
		`CREATE TABLE wire_submissions_raw(report_seq INT,created_at DATETIME,INDEX report_seq_index(report_seq))`,
		`CREATE TABLE external_ingest_index(seq INT,created_at DATETIME,INDEX seq_index(seq))`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal("fixture requires empty local database")
		}
		created = append(created, strings.Split(strings.Fields(statement)[2], "(")[0])
	}
	// CREATE column syntax above has no space before '('; retain explicit names.
	created = []string{"reports", "report_analysis", "wire_submissions_raw", "external_ingest_index"}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_evidence'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("fixture refuses preexisting evidence table")
	}
	if err := evidence.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	created = append(created, "report_evidence")
	store := &Database{db: db}
	empty, err := store.ScanPendingEvidenceReports(context.Background(), 10, EvidenceScanCursor{})
	if err != nil || empty.MoreToScan || len(empty.Items) != 0 {
		t.Fatal("empty unsigned table failed")
	}
	values := make([]string, 0, 2000)
	for seq := 58001; seq <= 60000; seq++ {
		values = append(values, fmt.Sprintf("(%d,'2000-01-01')", seq))
	}
	statements := []string{
		"INSERT INTO reports VALUES " + strings.Join(values, ","),
		`INSERT INTO report_analysis SELECT seq,'en','grok' FROM reports`,
		`INSERT INTO wire_submissions_raw SELECT seq,'2000-01-01' FROM reports`,
		`INSERT INTO external_ingest_index SELECT seq,'2000-01-01' FROM reports`,
		`INSERT INTO report_evidence(seq,evidence_version) SELECT seq,1 FROM reports`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal("cannot populate compact completed fixture")
		}
	}
	fetches := func() map[string]uint64 {
		rows, err := db.Query(`SELECT OBJECT_NAME,COUNT_FETCH FROM performance_schema.table_io_waits_summary_by_table WHERE OBJECT_SCHEMA=DATABASE() AND OBJECT_NAME IN ('reports','report_analysis','wire_submissions_raw','external_ingest_index')`)
		if err != nil {
			t.Fatal("cannot read local lookup counters")
		}
		defer rows.Close()
		out := map[string]uint64{}
		for rows.Next() {
			var name string
			var count uint64
			if err := rows.Scan(&name, &count); err != nil {
				t.Fatal(err)
			}
			out[name] = count
		}
		return out
	}
	before := fetches()
	batch, err := store.ScanPendingEvidenceReports(context.Background(), 10, EvidenceScanCursor{})
	if err != nil || len(batch.Items) != 0 || !batch.MoreToScan || batch.NextCursor.Before != 58000 {
		t.Fatalf("four-chunk empty round wrong: %+v err=%v", batch, err)
	}
	after := fetches()
	for _, name := range []string{"report_analysis", "wire_submissions_raw", "external_ingest_index"} {
		if after[name]-before[name] != 0 {
			t.Fatalf("completed rows still caused %s fetches", name)
		}
	}
	reportFetches := after["reports"] - before["reports"]
	if reportFetches > 4*(500+2)+2 {
		t.Fatalf("round exceeded2000-position read bound: reports_fetch=%d", reportFetches)
	}
	var examined uint64
	err = db.QueryRow(`SELECT COALESCE(SUM(ROWS_EXAMINED),0) FROM performance_schema.events_statements_history WHERE THREAD_ID=(SELECT THREAD_ID FROM performance_schema.threads WHERE PROCESSLIST_ID=CONNECTION_ID()) AND EVENT_NAME='statement/sql/select' AND SQL_TEXT LIKE 'SELECT /*+ MAX_EXECUTION_TIME(8000) */ r.seq,%'`).Scan(&examined)
	if err != nil || examined == 0 || examined > 4*500*2+32 {
		t.Fatalf("bounded PFS rows examined proof failed: rows=%d err=%v", examined, err)
	}
	t.Logf("bounded_round_reports_fetch=%d candidate_rows_examined=%d completed_RA_Wire_EII_fetch=0", reportFetches, examined)
	var encoded string
	if err := db.QueryRow("EXPLAIN FORMAT=JSON "+pendingEvidenceChunkQuery, 59500, 60000, 10).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(encoded), &plan); err != nil {
		t.Fatal(err)
	}
	sortUsed, primary, backward := false, false, false
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if node["using_filesort"] == true {
				sortUsed = true
			}
			if node["table_name"] == "r" && node["key"] == "PRIMARY" {
				primary = true
				backward = node["backward_index_scan"] == true
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
	if sortUsed || !primary || !backward {
		t.Fatal("bounded query must scan descending PRIMARY without filesort")
	}
	if path := os.Getenv("EVIDENCE_SCAN_PLAN_PATH"); path != "" {
		if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
			t.Fatal("cannot retain private fixture plan")
		}
	}
	for _, statement := range []string{
		`INSERT INTO reports VALUES (10001,'2000-01-01'),(10003,'2000-01-01'),(30001,'2000-01-01'),(30002,'2000-01-01'),(60001,UTC_TIMESTAMP()),(60002,UTC_TIMESTAMP())`,
		`INSERT INTO report_analysis VALUES (10001,'en','grok'),(10003,'en','claude'),(30001,'en','openai'),(30001,'de','openai'),(30002,'en','grok'),(60001,'en','grok')`,
		`INSERT INTO wire_submissions_raw VALUES (10001,UTC_TIMESTAMP()),(30001,UTC_TIMESTAMP()),(59999,UTC_TIMESTAMP()),(59998,UTC_TIMESTAMP())`,
		`INSERT INTO external_ingest_index VALUES (10003,UTC_TIMESTAMP()),(30002,'2000-01-01')`,
		`UPDATE report_evidence SET evidence_version=0 WHERE seq=59999`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal("cannot populate exact eligibility fixture")
		}
	}
	oracle := pendingEvidenceFixtureQuery(t, db, legacyPendingEvidenceQuery, 50)
	want := []PendingEvidence{{Seq: 60001, Brand: "grok"}, {Seq: 59999, Brand: "grok"}, {Seq: 30001, Brand: "openai"}, {Seq: 10003, Brand: "claude"}}
	if !reflect.DeepEqual(oracle, want) {
		t.Fatalf("legacy eligibility oracle changed: %+v", oracle)
	}
	seen := []PendingEvidence{}
	cursor := EvidenceScanCursor{}
	polls := 0
	for ; polls < 150; polls++ {
		next, err := store.ScanPendingEvidenceReports(context.Background(), 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = next.NextCursor
		if len(next.Items) > 2 || len(cursor.RetryHigh) > 101 {
			t.Fatal("batch/anchor cap changed")
		}
		for i, item := range next.Items {
			if i > 0 && next.Items[i-1].Seq <= item.Seq {
				t.Fatal("round items not unique descending")
			}
			seen = append(seen, item)
			if err := evidence.Save(context.Background(), db, item.Seq, evidence.Unknown("Completed fixture classification"), evidence.Version); err != nil {
				t.Fatal(err)
			}
		}
		if !next.MoreToScan && len(next.Items) == 0 {
			break
		}
	}
	if polls == 150 {
		t.Fatal("bounded cursor never completed full sweep")
	}
	// Full sweeps may return different polls; compare exact eligible identities.
	sortPending := func(items []PendingEvidence) {
		for i := range items {
			for j := i + 1; j < len(items); j++ {
				if items[j].Seq > items[i].Seq {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
	}
	sortPending(seen)
	if !reflect.DeepEqual(seen, oracle) {
		t.Fatalf("bounded sweep lost/duplicated eligible original rows: %+v", seen)
	}
	t.Logf("filesort=false descending_PRIMARY=true exact_full_window_recall=%d polls=%d", len(seen), polls+1)
}
