package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// Reuse the guarded disposable fixture's nullable, duplicate-capable tables.
// The unchanged COUNT-based reader remains the publication oracle.
func testProjectedEvidencePublicationBlockerParityMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	ids := []int{}
	for seq := 6850; seq <= 6867; seq++ {
		ids = append(ids, seq)
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,'2026-10-15',0,0,'')`, seq, fmt.Sprint("publication_", seq), fmt.Sprint("rpt_publication_", seq))
		exec(`INSERT INTO report_analysis VALUES(?,'en','publication','Publication',TRUE,'digital','2026-10-15',?,'Exact blocker semantics',0.5)`, seq, fmt.Sprint("Publication ", seq))
		exec(`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,entity_relationship,evidence_version) VALUES(?,'firsthand','Own observation','affected',1)`, seq)
	}
	exec(`INSERT INTO report_status VALUES
 (NULL,'resolved'),(6851,NULL),(6852,'active'),(6852,'active'),(6853,'active'),(6853,NULL),
 (6854,'active'),(6854,'resolved'),(6855,'resolved'),(6855,'resolved'),(6856,NULL),(6856,'resolved'),
 (6857,'ACTIVE'),(6858,'active '),(6859,''),(6867,'active')`)
	exec(`INSERT INTO reports_owners VALUES
 (NULL,'unrelated-private',FALSE),(6860,NULL,FALSE),(6861,'',FALSE),(6862,'unknown-publication',NULL),
 (6863,'named-public',TRUE),(6863,'named-private',FALSE),(6864,' ',FALSE),
 (6865,'duplicate-private',FALSE),(6865,'duplicate-private',FALSE),(6867,'named-public',TRUE)`)
	exec(`INSERT INTO report_raw(report_seq,visibility) VALUES(6866,'quarantined'),(6867,'public')`)
	r, err := RefreshEvidenceSourceProjection(context.Background(), db, ids)
	if err != nil || r.Computed != len(ids) || r.MissingParents != 0 {
		t.Fatalf("warm exact publication scope: %+v %v", r, err)
	}
	d := &Database{db: db}
	clock := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	check := func(filter EvidenceFilter, expected int) EvidenceBrandPage {
		t.Helper()
		got, err := d.getEvidenceBrandPageProjected(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		want, err := d.getEvidenceBrandPageSQL(context.Background(), filter)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("existence guards differ from original COUNT privacy contract: %v\ngot=%+v\nwant=%+v", err, got, want)
		}
		if got.Overview.TotalReports != expected || got.KindCounts.All != expected || got.KindCounts.Firsthand != expected || got.KindCounts.Coverage != 0 || got.KindCounts.Unknown != 0 {
			t.Fatal("NULL/mixed/duplicate/status/owner publication scope changed")
		}
		return got
	}
	filter := EvidenceFilter{BrandName: "publication", Language: "en", Kind: "all", Sort: "recent", Limit: 50, asOf: clock}
	got := check(filter, 9)
	wantVisible := map[int]bool{6850: true, 6851: true, 6852: true, 6853: true, 6857: true, 6860: true, 6861: true, 6862: true, 6867: true}
	if len(got.Reports) != len(wantVisible) {
		t.Fatal("a duplicate blocker multiplied rows or a NULL changed visibility")
	}
	for _, report := range got.Reports {
		if !wantVisible[report.Report.Seq] {
			t.Fatal("non-active, nonempty private owner, or quarantined report escaped")
		}
		delete(wantVisible, report.Report.Seq)
	}
	if len(wantVisible) != 0 {
		t.Fatal("active/NULL/unowned/public report was hidden")
	}
	for _, kind := range []string{"all", "firsthand", "coverage", "unknown"} {
		for _, sortBy := range []string{"recent", "signal", "recurrence"} {
			for _, offset := range []int{0, 2, 100} {
				check(EvidenceFilter{BrandName: "publication", Language: "en", Kind: kind, Sort: sortBy, Limit: 2, Offset: offset, asOf: clock}, 9)
			}
		}
	}
	// Cache warmth cannot freeze live publication. Both updates must exclude
	// their report immediately without changing or refreshing normalized sources.
	exec(`INSERT INTO reports_owners VALUES(6850,'new-private-owner',FALSE)`)
	check(filter, 8)
	exec(`UPDATE reports_owners SET is_public=TRUE WHERE seq=6850`)
	check(filter, 9)
	exec(`INSERT INTO report_status VALUES(6852,'resolved')`)
	check(filter, 8)
	exec(`DELETE FROM report_status WHERE seq=6852 AND status='resolved'`)
	check(filter, 9)
}
