package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

// A native type conversion can be represented as ref:[const,func]. Accept it
// only when this owner node still explicitly compares its seq to the outer ra.
// The optional schema qualifiers and backticks differ between MySQL plans.
var projectedOwnerSeqEquality = regexp.MustCompile(`(?:^|\()(?:[a-z0-9_$]+\.)?ro\.seq=(?:[a-z0-9_$]+\.)?ra\.seq(?:$|\))|(?:^|\()(?:[a-z0-9_$]+\.)?ra\.seq=(?:[a-z0-9_$]+\.)?ro\.seq(?:$|\))`)

func projectedOwnerSeqCorrelation(node map[string]any) bool {
	condition, _ := node["attached_condition"].(string)
	normalized := strings.Map(func(r rune) rune {
		if r == '`' || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, condition)
	return projectedOwnerSeqEquality.MatchString(normalized)
}

func projectedOwnerPointLookup(node map[string]any, dependent bool) bool {
	parts, _ := node["used_key_parts"].([]any)
	refs, _ := node["ref"].([]any)
	if !dependent || node["access_type"] != "ref" || node["key"] != "idx_is_public" || node["using_index"] != true ||
		len(parts) != 2 || parts[0] != "is_public" || parts[1] != "seq" || len(refs) != 2 || refs[0] != "const" {
		return false
	}
	outer, _ := refs[1].(string)
	return strings.HasSuffix(outer, ".seq") || outer == "func" && projectedOwnerSeqCorrelation(node)
}

func TestProjectedOwnerPointLookupPlanRepresentations(t *testing.T) {
	base := map[string]any{"access_type": "ref", "key": "idx_is_public", "using_index": true,
		"used_key_parts": []any{"is_public", "seq"}, "ref": []any{"const", "fixture.ra.seq"}}
	for _, tc := range []struct {
		name, ref, condition string
		dependent, want      bool
	}{
		{"direct outer reference", "fixture.ra.seq", "", true, true},
		{"native conversion", "func", "((`fixture`.`ro`.`seq` = `fixture`.`ra`.`seq`) and (`fixture`.`ro`.`owner` <> ''))", true, true},
		{"unqualified reverse equality", "func", "(ra.seq = ro.seq)", true, true},
		{"unproven conversion", "func", "(ro.owner <> '')", true, false},
		{"different outer key", "func", "(ro.seq = ra.other_seq)", true, false},
		{"different outer alias", "func", "(ro.seq = unrelated.seq)", true, false},
		{"computed different key", "func", "(ro.seq = ra.seq + 1)", true, false},
		{"uncorrelated lookup", "fixture.ra.seq", "(ro.seq = ra.seq)", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := make(map[string]any, len(base)+1)
			for key, value := range base {
				node[key] = value
			}
			node["ref"], node["attached_condition"] = []any{"const", tc.ref}, tc.condition
			if got := projectedOwnerPointLookup(node, tc.dependent); got != tc.want {
				t.Fatalf("point lookup=%t, want %t", got, tc.want)
			}
		})
	}
	for _, change := range []map[string]any{{"access_type": "ALL"}, {"using_index": false}, {"key": "PRIMARY"}, {"used_key_parts": []any{"is_public"}}, {"ref": []any{"const", "const"}}} {
		node := make(map[string]any, len(base))
		for key, value := range base {
			node[key] = value
		}
		for key, value := range change {
			node[key] = value
		}
		if projectedOwnerPointLookup(node, true) {
			t.Fatalf("accepted an unbounded or noncovering owner lookup: %+v", change)
		}
	}
}

// Called only by the guarded disposable dual-MySQL fixture. Its original
// nullable/duplicate-capable ownership schema remains index-free initially.
func testProjectedEvidenceOptionalOwnerIndexMySQL(t *testing.T, db *sql.DB) {
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
		// These unrelated blockers must not enter the brand scope or turn
		// the indexed correlated lookup into a global ownership scan.
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
			t.Fatalf("optional owner index changed public fields/facets/privacy: %v\ngot: %+v\nwant: %+v", err, got, want)
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
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var rawPlan string
			if err := conn.QueryRowContext(context.Background(), "EXPLAIN FORMAT=JSON "+query, args...).Scan(&rawPlan); err != nil {
				conn.Close()
				t.Fatal(err)
			}
			warnings, err := conn.QueryContext(context.Background(), "SHOW WARNINGS")
			if err != nil {
				conn.Close()
				t.Fatal(err)
			}
			defer warnings.Close()
			ignoredIndex := false
			for warnings.Next() {
				var level, message string
				var code int
				if err := warnings.Scan(&level, &code, &message); err != nil {
					t.Fatal(err)
				}
				ignoredIndex = ignoredIndex || level == "Warning" && strings.Contains(message, "idx_is_public")
			}
			err = warnings.Err()
			warnings.Close()
			conn.Close()
			// Missing optional hints may warn without changing query semantics.
			// Portability is proved by the plan and full oracle parity; when the
			// index exists it must resolve without an index-resolution warning.
			if err != nil || indexed && ignoredIndex {
				t.Fatalf("optional-index resolution warning mismatch: indexed=%t ignored=%t err=%v", indexed, ignoredIndex, err)
			}
			var plan any
			if err := json.Unmarshal([]byte(rawPlan), &plan); err != nil {
				t.Fatal(err)
			}
			ownerIndex, ownerPointLookup := false, false
			var ownerNodes []map[string]any
			var walk func(any, bool)
			walk = func(node any, dependent bool) {
				switch value := node.(type) {
				case map[string]any:
					dependent = dependent || value["dependent"] == true
					if value["table_name"] == "ro" {
						// Keep diagnostics to plan structure; never print the SQL
						// condition or the full plan on fixture failure.
						metadata := map[string]any{"dependent": dependent, "explicit_seq_correlation": projectedOwnerSeqCorrelation(value)}
						for _, key := range []string{"table_name", "access_type", "key", "used_key_parts", "ref", "using_index"} {
							metadata[key] = value[key]
						}
						ownerNodes = append(ownerNodes, metadata)
						if value["key"] == "idx_is_public" {
							ownerIndex = true
							ownerPointLookup = ownerPointLookup || projectedOwnerPointLookup(value, dependent)
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
			if ownerIndex != indexed || indexed && !ownerPointLookup {
				t.Fatalf("owner count lost its dependent covering const+seq lookup or used a missing index: indexed=%t owners=%+v", indexed, ownerNodes)
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
