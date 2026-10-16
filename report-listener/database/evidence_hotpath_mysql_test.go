package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"cleanapp-common/eiiheader"
)

// Uses the existing guarded disposable fixture, on both production-compatible
// MySQL versions. The unchanged COUNT reference remains the visibility oracle.
func testEvidenceMetadataBlockerParityMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Fresh parent columns and the legacy projection can have different
	// collations. Bulk visibility must compare within the selected source column
	// rather than aggregate them in a character-valued CASE before comparison.
	exec(`ALTER TABLE reports MODIFY COLUMN id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL DEFAULT '', MODIFY COLUMN public_id VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`)
	ids := []int{}
	for seq := 6200; seq < 6220; seq++ {
		ids = append(ids, seq)
		media := []byte{}
		if seq%2 == 0 {
			media = []byte("retained media")
		}
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,'2026-10-15',47.5,8.5,?)`, seq, fmt.Sprint("original_", seq), fmt.Sprint("guard_", seq), media)
		exec(`INSERT INTO report_analysis VALUES(?,'en','blockerparity','Visibility',TRUE,'digital','2026-10-15','Original issue','Summary',0.5)`, seq)
	}
	for _, statement := range []string{
		`INSERT INTO report_status VALUES(6201,'active'),(6202,NULL),(6203,'active'),(6203,'resolved'),(6204,'resolved'),(6204,'deleted'),(6205,''),(6219,'active'),(6219,NULL)`,
		`INSERT INTO reports_owners VALUES(6206,'public-owner',TRUE),(6207,NULL,FALSE),(6208,'',FALSE),(6209,' ',FALSE),(6210,'public-owner',TRUE),(6210,'private-owner',FALSE),(6211,'private-one',FALSE),(6211,'private-two',FALSE),(6212,'unknown-publicity',NULL),(6213,'',FALSE),(6213,NULL,FALSE),(6213,'public-owner',TRUE),(6219,'public-one',TRUE),(6219,'public-two',TRUE)`,
		`INSERT INTO report_raw VALUES(6214,'sensor','raw','fetcher','quarantine'),(6215,'sensor','raw','fetcher','shadow'),(6216,'sensor','raw','fetcher',NULL),(6217,'sensor','raw','fetcher','public')`,
		`UPDATE reports SET public_id='' WHERE seq=6218`,
	} {
		exec(statement)
	}
	for _, mode := range []string{"warm", "mixed_projection_fallback"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "mixed_projection_fallback" {
				exec(`DELETE FROM report_metadata_projection WHERE seq IN (6200,6202,6206)`)
			}
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			rows, err := tx.Query(`SELECT ids.seq FROM `+evidenceIDsTable+`
 JOIN reports r ON r.seq=ids.seq LEFT JOIN report_raw rr ON rr.report_seq=r.seq
 WHERE r.public_id<>'' AND `+PublicVisibilityWhereSQL+`
 AND (SELECT COUNT(*) FROM report_status rs WHERE rs.seq=r.seq AND rs.status IS NOT NULL AND rs.status<>'active')=0
 AND (SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq=r.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)=0`, evidenceIDsJSON(ids))
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			var want []int
			for rows.Next() {
				var seq int
				if err := rows.Scan(&seq); err != nil {
					t.Fatal(err)
				}
				want = append(want, seq)
			}
			rowErr := rows.Err()
			rows.Close()
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			records, sources, visible, err := loadEvidenceMetadataCore(context.Background(), tx, ids)
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err != nil {
				t.Fatal(err)
			}
			sort.Ints(visible)
			sort.Ints(want)
			if !reflect.DeepEqual(visible, want) || len(records) != len(want) || len(sources) != len(want) {
				t.Fatalf("blocker joins differ from COUNT reference: visible=%v want=%v", visible, want)
			}
			for _, seq := range want {
				r, source := records[seq], sources[seq]
				if r.Seq != seq || r.PublicID != fmt.Sprint("guard_", seq) || r.Latitude != 47.5 || r.Longitude != 8.5 || r.HasOriginalMedia != (seq%2 == 0) || source.Seq != seq || source.ReportID != fmt.Sprint("original_", seq) {
					t.Fatal("blocker joins changed visible metadata/source maps")
				}
			}
			// Also compare every public field/count against the complete unchanged
			// SQL reader. Space-owner behavior follows the actual DB collation.
			d := &Database{db: db}
			f := EvidenceFilter{BrandName: "blockerparity", Kind: "all", Sort: "recent", Limit: 50}
			actual, err := d.GetEvidenceBrandPage(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := d.getEvidenceBrandPageSQL(context.Background(), f)
			if err != nil || !evidencePagesEqualIgnoringClock(actual, reference) {
				t.Fatalf("public blocker/count parity differs: %v", err)
			}
		})
	}
	t.Run("MixedCollationPublicIDBranchAuthority", func(t *testing.T) {
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(6230,'original_6230','guard_6230','2026-10-15',47.5,8.5,'')`)
		check := func(want bool, publicID string) {
			t.Helper()
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			records, _, visible, err := loadEvidenceMetadataCore(context.Background(), tx, []int{6230})
			if closeErr := tx.Rollback(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil || (len(visible) == 1) != want || len(visible) > 1 {
				t.Fatalf("mixed collation public-ID visibility: want=%v rows=%d err=%v", want, len(visible), err)
			}
			if want && records[6230].PublicID != publicID {
				t.Fatal("mixed collation lookup rewrote the public ID")
			}
		}
		check(true, "guard_6230")
		exec(`UPDATE report_metadata_projection SET public_id=NULL WHERE seq=6230`)
		check(false, "") // An authoritative NULL must not resurrect the parent ID.
		exec(`DELETE FROM report_metadata_projection WHERE seq=6230`)
		check(true, "guard_6230")
		for _, value := range []any{"", nil} {
			exec(`UPDATE reports SET public_id=? WHERE seq=6230`, value)
			check(false, "")
			exec(`DELETE FROM report_metadata_projection WHERE seq=6230`)
			check(false, "")
		}
		exec(`UPDATE reports SET public_id=' ' WHERE seq=6230`)
		var projectedVisible, parentVisible bool
		if err := db.QueryRow(`SELECT m.public_id<>'',r.public_id<>'' FROM report_metadata_projection m JOIN reports r ON r.seq=m.seq WHERE r.seq=6230`).Scan(&projectedVisible, &parentVisible); err != nil {
			t.Fatal(err)
		}
		check(projectedVisible, " ")
		exec(`DELETE FROM report_metadata_projection WHERE seq=6230`)
		check(parentVisible, " ") // Retain each source's actual PAD/NO PAD rules.
	})
}

func testEvidenceIndexWinnerParityMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	ids := []int{6300, 6301, 6302, 6303, 6304}
	for _, seq := range ids {
		exec(`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(?,?,'2026-10-15',0,0,'')`, seq, fmt.Sprint("source_", seq))
	}
	exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url,source_timestamp,created_at) VALUES
 (6300,'single','sole','https://example.test/sole',NULL,'2026-10-15'),
 (6301,'zeta','later-source','https://example.test/later-source',NULL,'2026-10-15'),
 (6301,'éclair','first-source','https://example.test/first-source','2026-01-01','2026-10-15'),
 (6302,'same','zeta','https://example.test/later-id',NULL,'2026-10-15'),
 (6302,'same','éclair','https://example.test/first-id','2026-02-01','2026-10-15'),
 (6303,'MixedCase','OriginalCase','https://example.test/original-case',NULL,'2026-10-15')`)
	var planJSON string
	if err := db.QueryRow(`EXPLAIN FORMAT=JSON `+evidenceIndexSourcesSQL, evidenceIDsJSON(ids)).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatal(err)
	}
	var coveredIdentity func(any) bool
	coveredIdentity = func(value any) bool {
		switch value := value.(type) {
		case map[string]any:
			if value["table_name"] == "e" && value["key"] == "idx_seq" && value["using_index"] == true {
				return true
			}
			for _, child := range value {
				if coveredIdentity(child) {
					return true
				}
			}
		case []any:
			for _, child := range value {
				if coveredIdentity(child) {
					return true
				}
			}
		}
		return false
	}
	if !coveredIdentity(plan) {
		t.Fatal("combined header read forced original identity parent-row reads")
	}
	for _, mode := range []string{"cold", "warm", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := eiiheader.Backfill(context.Background(), db, ids); err != nil {
				t.Fatal(err)
			}
			if mode == "cold" {
				exec(`DELETE FROM external_ingest_header_projection WHERE seq BETWEEN 6300 AND 6304`)
			} else if mode == "mixed" {
				exec(`DELETE FROM external_ingest_header_projection WHERE seq IN (6300,6302)`)
			}
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			rows, err := tx.Query(`SELECT e.seq,e.source,e.external_id,e.source_url,DATE_FORMAT(e.source_timestamp,'%Y-%m-%d %H:%i:%s.%f'),DATE_FORMAT(e.created_at,'%Y-%m-%d %H:%i:%s.%f') FROM `+evidenceIDsTable+` JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq ORDER BY e.source,e.external_id`, evidenceIDsJSON(ids))
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			want := map[int]evidenceSourceNormalizationRow{}
			for rows.Next() {
				var seq int
				var source, id, url, published, created sql.NullString
				if err := rows.Scan(&seq, &source, &id, &url, &published, &created); err != nil {
					t.Fatal(err)
				}
				if _, exists := want[seq]; !exists {
					want[seq] = evidenceSourceNormalizationRow{EiiSource: nullableEvidenceString(source), EiiID: nullableEvidenceString(id), EiiURL: nullableEvidenceString(url), EiiPublished: nullableEvidenceString(published), EiiCreated: nullableEvidenceString(created)}
				}
			}
			rowErr := rows.Err()
			rows.Close()
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			got := map[int]evidenceSourceNormalizationRow{}
			err = loadEvidenceIndexSources(context.Background(), tx, ids, got)
			if closeErr := tx.Rollback(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("source winner/header maps differ from global SQL-collated reference in %s mode: %v", mode, err)
			}
			if *got[6301].EiiSource != "éclair" || *got[6302].EiiID != "éclair" || *got[6303].EiiID != "OriginalCase" {
				t.Fatal("source selection used Go order or rewrote source identity")
			}
		})
	}
	t.Run("ProjectedNullsRemainAuthoritative", func(t *testing.T) {
		if _, err := eiiheader.Backfill(context.Background(), db, []int{6300}); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE external_ingest_header_projection SET source_url=NULL,source_timestamp=NULL,created_at=NULL WHERE seq=6300`)
		for _, projected := range []bool{true, false} {
			if !projected {
				exec(`DELETE FROM external_ingest_header_projection WHERE seq=6300`)
			}
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			sources := map[int]evidenceSourceNormalizationRow{}
			err = loadEvidenceIndexSources(context.Background(), tx, []int{6300}, sources)
			if closeErr := tx.Rollback(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil {
				t.Fatal(err)
			}
			source := sources[6300]
			if projected && (source.EiiURL != nil || source.EiiPublished != nil || source.EiiCreated != nil) {
				t.Fatal("projected SQL NULL resurrected a parent source value")
			}
			if !projected && (source.EiiURL == nil || *source.EiiURL != "https://example.test/sole" || source.EiiCreated == nil) {
				t.Fatal("absent source projection lost its parent fallback")
			}
		}
	})
}
