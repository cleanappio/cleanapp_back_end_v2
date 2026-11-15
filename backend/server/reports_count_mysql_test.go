package server

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Opt-in only: owns two new tables in a dedicated local disposable schema.
// This fixture never connects to production or creates reports/provider work.
func TestReportCounterMySQLReconciliation(t *testing.T) {
	dsn := os.Getenv("REPORT_COUNTER_TEST_DSN")
	if dsn == "" {
		t.Skip("REPORT_COUNTER_TEST_DSN is not configured")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid report counter fixture DSN")
	}
	host, _, err := net.SplitHostPort(config.Addr)
	if err != nil || config.Net != "tcp" || (host != "127.0.0.1" && host != "localhost" && host != "::1") || config.DBName != "report_counter_test" {
		t.Fatal("report counter fixture requires local TCP and the dedicated report_counter_test schema")
	}
	dbc, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("could not open the report counter fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var created []string
	t.Cleanup(func() {
		defer dbc.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, table := range created {
			_, _ = dbc.ExecContext(cleanupCtx, "DROP TABLE "+table)
		}
	})
	exec := func(query string) {
		t.Helper()
		if _, err := dbc.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	// No IF NOT EXISTS or initial DROP: refuse an already populated schema.
	exec(`CREATE TABLE report_analysis (
		seq INT NOT NULL, language VARCHAR(8), is_valid BOOLEAN, classification VARCHAR(16),
		INDEX idx_ra_seq_class_valid(seq, classification, is_valid)
	)`)
	created = append(created, "report_analysis")
	exec(`CREATE TABLE report_counts_total (
		id TINYINT PRIMARY KEY, total_valid BIGINT NOT NULL, physical_valid BIGINT NOT NULL,
		digital_valid BIGINT NOT NULL, last_counted_seq INT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	)`)
	created = append(created, "report_counts_total")
	exec(`INSERT INTO report_analysis(seq,language,is_valid,classification) VALUES
		(1,'en',TRUE,'physical'), (1,'es',TRUE,'physical'),
		(2,'en',TRUE,'digital'), (3,'en',FALSE,'physical'),
		(4,'en',TRUE,'physical'), (4,'fr',TRUE,'digital'), (100,'en',TRUE,'digital')`)

	cache := &reportCountCache{}
	check := func(total, physical, digital int) {
		t.Helper()
		if err := refreshCounterCache(ctx, dbc, cache, 2, nil); err != nil {
			t.Fatal(err)
		}
		if cache.totalReports != total || cache.physicalCount != physical || cache.digitalCount != digital {
			t.Fatalf("got total/physical/digital=%d/%d/%d; want %d/%d/%d", cache.totalReports, cache.physicalCount, cache.digitalCount, total, physical, digital)
		}
		var storedTotal, storedPhysical, storedDigital int
		if err := dbc.QueryRowContext(ctx, "SELECT total_valid, physical_valid, digital_valid FROM report_counts_total WHERE id=1").Scan(&storedTotal, &storedPhysical, &storedDigital); err != nil {
			t.Fatal(err)
		}
		if storedTotal != total || storedPhysical != physical || storedDigital != digital {
			t.Fatal("durable readers did not receive the complete reconciled snapshot")
		}
	}
	check(4, 2, 3) // Distinct seq across languages; one seq can count in both classes.
	assertCounterCoveringPlan(t, ctx, dbc)
	exec(`INSERT INTO report_analysis VALUES(5,'en',TRUE,'physical')`)
	exec(`UPDATE report_analysis SET is_valid=TRUE WHERE seq=3`)
	check(6, 4, 3) // Late analysis below unchanged MAX(seq)=100 is recovered.
	exec(`UPDATE report_analysis SET classification='physical' WHERE seq=2`)
	check(6, 5, 2)
	exec(`DELETE FROM report_analysis WHERE seq=1`)
	check(5, 4, 2)
	exec(`UPDATE report_analysis SET is_valid=FALSE WHERE seq=4`)
	check(4, 3, 1)
	exec(`DELETE FROM report_analysis`)
	check(0, 0, 0)
}

func assertCounterCoveringPlan(t *testing.T, ctx context.Context, dbc *sql.DB) {
	t.Helper()
	rows, err := dbc.QueryContext(ctx, "EXPLAIN "+reportCounterPageSQL, 0, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		plan := map[string]string{}
		for i, column := range columns {
			if raw, ok := values[i].([]byte); ok {
				plan[column] = string(raw)
			} else if values[i] != nil {
				plan[column] = fmt.Sprint(values[i])
			}
		}
		if plan["table"] != "report_analysis" {
			continue
		}
		found = true
		if plan["key"] != "idx_ra_seq_class_valid" || plan["type"] != "range" || !strings.Contains(strings.ToLower(plan["Extra"]), "using index") || strings.Contains(strings.ToLower(plan["Extra"]), "filesort") {
			t.Fatalf("counter page did not use the ordered covering range: %+v", plan)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("counter EXPLAIN omitted its source table")
	}
}
