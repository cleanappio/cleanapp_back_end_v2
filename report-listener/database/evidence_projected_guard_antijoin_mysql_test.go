package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// projectedGuardTables are the publication blockers the projected base query
// expresses as NOT EXISTS anti-joins: inactive status, private owner, and
// non-public raw visibility. Each must survive as a plan node that is not a
// per-report dependent subquery, so the optimizer stays free to materialise the
// (measured tiny or empty) blocking set once per query instead of running one
// indexed lookup per cohort row.
var projectedGuardTables = []string{"rs", "ro", "rr"}

type projectedGuardPlanShape struct {
	present, dependent bool
}

// projectedGuardPlanShapes walks an EXPLAIN FORMAT=JSON plan and records, for
// each guard table alias, whether it appears at all and whether any occurrence
// sits under a dependent (per-outer-row) subquery block.
func projectedGuardPlanShapes(plan any) map[string]projectedGuardPlanShape {
	shapes := map[string]projectedGuardPlanShape{}
	for _, alias := range projectedGuardTables {
		shapes[alias] = projectedGuardPlanShape{}
	}
	var walk func(any, bool)
	walk = func(node any, dependent bool) {
		switch value := node.(type) {
		case map[string]any:
			dependent = dependent || value["dependent"] == true
			if alias, ok := value["table_name"].(string); ok {
				if shape, tracked := shapes[alias]; tracked {
					shape.present = true
					shape.dependent = shape.dependent || dependent
					shapes[alias] = shape
				}
			}
			for _, child := range value {
				walk(child, dependent)
			}
		case []any:
			for _, child := range value {
				walk(child, dependent)
			}
		}
	}
	walk(plan, false)
	return shapes
}

func TestProjectedGuardPlanShapeRepresentations(t *testing.T) {
	table := func(alias string) map[string]any {
		return map[string]any{"table": map[string]any{"table_name": alias, "access_type": "ALL"}}
	}
	outer := map[string]any{"table": map[string]any{"table_name": "ra", "access_type": "ALL"}}
	for _, tc := range []struct {
		name      string
		plan      map[string]any
		dependent bool
	}{
		{"materialised anti-join", map[string]any{"query_block": map[string]any{"nested_loop": []any{outer,
			map[string]any{"table": map[string]any{"table_name": "<subquery2>", "access_type": "eq_ref", "key": "<auto_distinct_key>",
				"materialized_from_subquery": map[string]any{"using_temporary_table": true, "dependent": false, "query_block": table("ro")}}}}}}, false},
		{"nested-loop anti-join", map[string]any{"query_block": map[string]any{"nested_loop": []any{outer, table("ro")}}}, false},
		{"dependent subquery", map[string]any{"query_block": map[string]any{"table": map[string]any{"table_name": "ra",
			"attached_subqueries": []any{map[string]any{"dependent": true, "cacheable": false, "query_block": table("ro")}}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shapes := projectedGuardPlanShapes(tc.plan)
			if !shapes["ro"].present || shapes["ro"].dependent != tc.dependent || shapes["rr"].present || shapes["rs"].present {
				t.Fatalf("plan shape misread: %+v", shapes)
			}
		})
	}
}

// Called only by the guarded disposable dual-MySQL fixture. Its original
// nullable/duplicate-capable ownership schema remains index-free initially; the
// second phase swaps in production's keyed ownership shape. Both phases must
// keep exact oracle parity and must not leave any guard as a per-report
// dependent subquery.
func testProjectedEvidenceGuardAntiJoinsMySQL(t *testing.T, db *sql.DB) {
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var initiallyIndexed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='reports_owners' AND INDEX_NAME='idx_is_public'`).Scan(&initiallyIndexed); err != nil || initiallyIndexed != 0 {
		t.Fatal("original index-free ownership fixture was changed")
	}
	ids := []int{}
	for seq := 6800; seq <= 6810; seq++ {
		ids = append(ids, seq)
		exec(t, `INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,'2026-10-15',0,0,'')`, seq, fmt.Sprint("owner_hint_", seq), fmt.Sprint("rpt_owner_hint_", seq))
		exec(t, `INSERT INTO report_analysis VALUES(?,'en','ownerhint','Owner hint',TRUE,'digital','2026-10-15',?,'Original summary',0.5)`, seq, fmt.Sprint("Owner title ", seq))
		exec(t, `INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES(?,'firsthand','Observed','Blocked','Original','affected',1)`, seq)
	}
	exec(t, `INSERT INTO reports_owners VALUES
 (6801,NULL,FALSE),(6802,'',FALSE),(6803,' ',FALSE),(6804,'unknown-publication',NULL),
 (6805,'duplicate-public',TRUE),(6805,'duplicate-public',TRUE),
 (6806,'duplicate-private',FALSE),(6806,'duplicate-private',FALSE),
 (6807,'mixed-public',TRUE),(6807,'mixed-private',FALSE),
 (6808,NULL,FALSE),(6808,'named-public',TRUE),(6809,'named-private',FALSE),
 (6810,'unknown-publication',NULL),(6810,'',FALSE)`)
	for seq := 6900; seq < 6964; seq++ {
		// These unrelated blockers enter the materialised private-owner set but
		// must not enter the brand scope or change any brand report's visibility.
		exec(t, `INSERT INTO reports_owners VALUES(?,'unrelated-private',FALSE)`, seq)
	}
	refresh, err := RefreshEvidenceSourceProjection(context.Background(), db, ids)
	if err != nil || refresh.Computed != len(ids) || refresh.MissingParents != 0 {
		t.Fatalf("actual warm source refresh failed: %+v %v", refresh, err)
	}
	d := &Database{db: db}
	clock := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	check := func(t *testing.T, filter EvidenceFilter, expected int) {
		t.Helper()
		got, err := d.getEvidenceBrandPageProjected(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		want, err := d.getEvidenceBrandPageSQL(context.Background(), filter)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("anti-join guards changed public fields/facets/privacy: %v\ngot: %+v\nwant: %+v", err, got, want)
		}
		if got.Overview.TotalReports != expected || got.KindCounts.All != expected || got.KindCounts.Firsthand != expected || got.KindCounts.Coverage != 0 || got.KindCounts.Unknown != 0 {
			t.Fatalf("NULL/empty/duplicate/mixed owner changed exact scope: %+v", got)
		}
	}
	for _, indexed := range []bool{false, true} {
		if indexed {
			// Keep the original NULL/duplicate-capable fixture intact. This
			// temporary table models production's implicit secondary PK keys.
			exec(t, `CREATE TABLE owner_hint_indexed_fixture(seq INT NOT NULL,owner VARCHAR(256) NOT NULL,is_public BOOL,PRIMARY KEY(seq,owner),INDEX idx_is_public(is_public)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
			t.Cleanup(func() {
				var backupExists int
				if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='owner_hint_original_fixture'`).Scan(&backupExists); err != nil {
					t.Errorf("inspect original ownership fixture backup: %v", err)
					return
				}
				if backupExists != 0 {
					if _, err := db.Exec(`RENAME TABLE reports_owners TO owner_hint_indexed_fixture,owner_hint_original_fixture TO reports_owners`); err != nil {
						t.Errorf("restore original index-free ownership fixture: %v", err)
						return
					}
				}
				if _, err := db.Exec(`DROP TABLE IF EXISTS owner_hint_indexed_fixture`); err != nil {
					t.Errorf("remove fixture-only indexed ownership table: %v", err)
				}
			})
			// NULL owners never block; a space owner remains nonempty under
			// production0900 NO PAD in both phases. Collapsing duplicates by MIN
			// retains any FALSE publication flag and preserves an all-NULL flag.
			exec(t, `INSERT INTO owner_hint_indexed_fixture SELECT seq,owner,MIN(is_public) FROM reports_owners WHERE (seq BETWEEN 6800 AND 6810 OR seq BETWEEN 6900 AND 6963) AND owner IS NOT NULL GROUP BY seq,owner`)
			exec(t, `RENAME TABLE reports_owners TO owner_hint_original_fixture,owner_hint_indexed_fixture TO reports_owners`)
		}
		t.Run(fmt.Sprintf("indexed_%t", indexed), func(t *testing.T) {
			filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "ownerhint", Language: "en", Kind: "all", Limit: 2, asOf: clock})
			query, args := projectedEvidenceBaseQuery(filter)
			var rawPlan string
			if err := db.QueryRow("EXPLAIN FORMAT=JSON "+query, args...).Scan(&rawPlan); err != nil {
				t.Fatal(err)
			}
			var plan any
			if err := json.Unmarshal([]byte(rawPlan), &plan); err != nil {
				t.Fatal(err)
			}
			// Keep diagnostics to plan structure; never print the SQL condition
			// or the full plan on fixture failure.
			shapes := projectedGuardPlanShapes(plan)
			for _, alias := range projectedGuardTables {
				if shape := shapes[alias]; !shape.present || shape.dependent {
					t.Fatalf("publication guard %s is missing or remains a per-report dependent subquery: indexed=%t shapes=%+v", alias, indexed, shapes)
				}
			}
			for _, kind := range []string{"all", "firsthand", "coverage", "unknown"} {
				for _, sortBy := range []string{"recent", "signal", "recurrence"} {
					for _, offset := range []int{0, 2, 100} {
						f := EvidenceFilter{BrandName: "ownerhint", Language: "en", Kind: kind, Sort: sortBy, Limit: 2, Offset: offset, asOf: clock}
						check(t, f, 7)
					}
				}
			}
			// The source tuple stays warm; owner publication must remain live.
			exec(t, `UPDATE reports_owners SET is_public=FALSE WHERE seq=6805`)
			check(t, filter, 6)
			exec(t, `UPDATE reports_owners SET is_public=TRUE WHERE seq=6805`)
		})
	}
}
