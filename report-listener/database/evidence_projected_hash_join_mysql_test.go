package database

import (
	"database/sql"
	"strings"
	"testing"
)

// The parity subtests above already run the page reader under this plan. This
// proves MySQL honors the hints: every compact per-report table is built once
// as a hash join instead of probed by PRIMARY for each cohort row.
func testProjectedEvidenceHashJoinPlanMySQL(t *testing.T, db *sql.DB) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "narrowidentity", Kind: "all", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	query, args := projectedEvidenceBaseQuery(f)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var plan string
	if err := conn.QueryRowContext(t.Context(), "EXPLAIN FORMAT=TREE "+query, args...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(t.Context(), "SHOW WARNINGS")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var level, message string
		var code int
		if err := rows.Scan(&level, &code, &message); err != nil {
			t.Fatal(err)
		}
		if level == "Warning" {
			t.Fatalf("optimizer rejected a projected reader hint: %d %s", code, message)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !strings.Contains(plan, "Hash") {
		t.Fatalf("projected base plan has no hash join:\n%s", plan)
	}
	for _, alias := range []string{"m", "rr", "ev", "p"} {
		if strings.Contains(plan, "lookup on "+alias+" using PRIMARY") || !strings.Contains(plan, " scan on "+alias) {
			t.Fatalf("projected base still probes %s per cohort row:\n%s", alias, plan)
		}
	}
	if !strings.Contains(plan, "on ra using "+evidenceBrandCoreIndex) {
		t.Fatalf("projected base no longer drives from the covering brand index:\n%s", plan)
	}
}
