package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

var notificationOwnerSeqEquality = regexp.MustCompile(`(?:^|\()(?:[a-z0-9_$]+\.)?nowner\.seq=(?:[a-z0-9_$]+\.)?r\.seq(?:$|\))|(?:^|\()(?:[a-z0-9_$]+\.)?r\.seq=(?:[a-z0-9_$]+\.)?nowner\.seq(?:$|\))`)

func notificationOwnerPointLookup(node map[string]any, dependent bool) bool {
	parts, _ := node["used_key_parts"].([]any)
	refs, _ := node["ref"].([]any)
	if !dependent || node["access_type"] != "ref" || node["key"] != "idx_is_public" || node["using_index"] != true ||
		len(parts) != 2 || parts[0] != "is_public" || parts[1] != "seq" || len(refs) != 2 || refs[0] != "const" {
		return false
	}
	outer, _ := refs[1].(string)
	condition, _ := node["attached_condition"].(string)
	normalized := strings.Map(func(r rune) rune {
		if r == '`' || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, condition)
	return strings.HasSuffix(outer, ".r.seq") || outer == "r.seq" || outer == "func" && notificationOwnerSeqEquality.MatchString(normalized)
}

func TestNotificationOwnerPointLookupRejectsGlobalOrUnprovenPlans(t *testing.T) {
	node := map[string]any{"access_type": "ref", "key": "idx_is_public", "using_index": true,
		"used_key_parts": []any{"is_public", "seq"}, "ref": []any{"const", "fixture.r.seq"}}
	if !notificationOwnerPointLookup(node, true) {
		t.Fatal("native correlated lookup rejected")
	}
	node["ref"] = []any{"const", "func"}
	node["attached_condition"] = "((`fixture`.`nowner`.`seq` = `fixture`.`r`.`seq`) and (`nowner`.`owner` <> ''))"
	if !notificationOwnerPointLookup(node, true) {
		t.Fatal("proven native conversion rejected")
	}
	for _, condition := range []string{"", "(nowner.seq=unrelated.seq)", "(nowner.seq=r.seq+1)"} {
		node["attached_condition"] = condition
		if notificationOwnerPointLookup(node, true) {
			t.Fatal("unproven correlation accepted")
		}
	}
	node["ref"] = []any{"const", "fixture.r.seq"}
	node["access_type"] = "ALL"
	if notificationOwnerPointLookup(node, true) {
		t.Fatal("global owner scan accepted")
	}
	node["access_type"] = "ref"
	if notificationOwnerPointLookup(node, false) {
		t.Fatal("uncorrelated owner lookup accepted")
	}
}

// Called only inside the guarded, empty localhost notification fixture already
// run by both MySQL CI jobs. Temporary policy tables are pinned to one physical
// connection and dropped before it is returned to the pool. No sender is called.
func testNotificationOwnerCorrelationMySQL(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := conn.ExecContext(cleanup, `DROP TEMPORARY TABLE IF EXISTS reports_owners,report_status,notification_owner_scope,notification_owner_input`); err != nil {
			t.Error(err)
		}
	}()
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	legacy := `NOT EXISTS (SELECT 1 FROM report_status ns WHERE ns.seq=r.seq AND ns.status IS NOT NULL AND ns.status<>'active')
        AND NOT EXISTS (SELECT 1 FROM reports_owners nowner WHERE nowner.seq=r.seq AND nowner.owner IS NOT NULL AND nowner.owner<>'' AND nowner.is_public=FALSE)`
	for _, collation := range []string{"utf8mb4_0900_ai_ci", "utf8mb4_unicode_ci"} {
		t.Run(collation, func(t *testing.T) {
			exec(t, `CREATE TEMPORARY TABLE notification_owner_scope(seq INT PRIMARY KEY)`)
			exec(t, `CREATE TEMPORARY TABLE report_status(seq INT,status VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE `+collation)
			exec(t, `CREATE TEMPORARY TABLE reports_owners(seq INT,owner VARCHAR(100),is_public BOOLEAN) DEFAULT CHARSET=utf8mb4 COLLATE `+collation)
			for seq := 20000; seq <= 20015; seq++ {
				exec(t, `INSERT INTO notification_owner_scope VALUES(?)`, seq)
			}
			exec(t, `INSERT INTO reports_owners VALUES
 (20001,'private',FALSE),(20002,'',FALSE),(20003,NULL,FALSE),(20004,'public',TRUE),(20005,'unset',NULL),
 (20006,'public',TRUE),(20006,'private',FALSE),(20007,'duplicate',FALSE),(20007,'duplicate',FALSE),
 (20008,' ',FALSE),(20009,NULL,FALSE),(20010,'',NULL)`)
			exec(t, `INSERT INTO report_status VALUES(20011,NULL),(20012,'active'),(20012,'resolved'),(20013,NULL),(20013,'active'),(20015,'active ')`)
			read := func(t *testing.T, policy string) []int {
				t.Helper()
				rows, err := conn.QueryContext(ctx, `SELECT r.seq FROM notification_owner_scope r WHERE `+policy+` ORDER BY r.seq`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var ids []int
				for rows.Next() {
					var seq int
					if err := rows.Scan(&seq); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, seq)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return ids
			}
			check := func(t *testing.T) {
				t.Helper()
				if !reflect.DeepEqual(read(t, legacy), read(t, notificationPublicStateSQL("r"))) {
					t.Fatal("owner optimization changed native NULL/duplicate/space/privacy policy")
				}
				var allowed bool
				if err := conn.QueryRowContext(ctx, `SELECT `+notificationPublicStateSQL("r")+` FROM (SELECT CAST(NULL AS SIGNED) AS seq) r`).Scan(&allowed); err != nil || !allowed {
					t.Fatal("NULL outer sequence policy changed")
				}
			}
			nativeAllowed := read(t, legacy)
			t.Run("optional_index_absent_nullable_duplicate_policy", func(t *testing.T) { check(t) })
			// Preserve the same collation, but use the actual production owner
			// key layout. NULL owners are ignored by both policies; duplicates
			// collapse only in this fixture's production-shaped copy.
			exec(t, `CREATE TEMPORARY TABLE notification_owner_input AS SELECT * FROM reports_owners`)
			exec(t, `DROP TEMPORARY TABLE reports_owners`)
			exec(t, `CREATE TEMPORARY TABLE reports_owners(seq INT NOT NULL,owner VARCHAR(100) NOT NULL,is_public BOOLEAN,PRIMARY KEY(seq,owner),KEY idx_is_public(is_public)) DEFAULT CHARSET=utf8mb4 COLLATE `+collation)
			exec(t, `INSERT INTO reports_owners SELECT DISTINCT seq,owner,is_public FROM notification_owner_input WHERE seq IS NOT NULL AND owner IS NOT NULL`)
			for seq := 30000; seq < 30064; seq++ {
				exec(t, `INSERT INTO reports_owners VALUES(?,'unrelated-private-owner',FALSE)`, seq)
			}
			t.Run("production_index_is_dependent_covering_const_and_sequence", func(t *testing.T) {
				check(t)
				if !reflect.DeepEqual(nativeAllowed, read(t, legacy)) {
					t.Fatal("production-shaped index copy changed policy inputs")
				}
				var raw []byte
				if err := conn.QueryRowContext(ctx, `EXPLAIN FORMAT=JSON SELECT r.seq FROM notification_owner_scope r WHERE `+notificationPublicStateSQL("r")).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var plan any
				if err := json.Unmarshal(raw, &plan); err != nil {
					t.Fatal(err)
				}
				var owners []map[string]any
				good := false
				var walk func(any, bool)
				walk = func(value any, dependent bool) {
					switch value := value.(type) {
					case map[string]any:
						dependent = dependent || value["dependent"] == true
						if value["table_name"] == "nowner" {
							metadata := map[string]any{"dependent": dependent}
							for _, key := range []string{"access_type", "key", "used_key_parts", "ref", "using_index"} {
								metadata[key] = value[key]
							}
							owners = append(owners, metadata)
							good = good || notificationOwnerPointLookup(value, dependent)
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
				if len(owners) != 1 || !good {
					t.Fatalf("owner guard lost dependent covered point lookup: %v", owners)
				}
				exec(t, `UPDATE reports_owners SET is_public=FALSE WHERE seq=20004`)
				check(t)
			})
			exec(t, `DROP TEMPORARY TABLE reports_owners,report_status,notification_owner_scope,notification_owner_input`)
		})
	}
}
