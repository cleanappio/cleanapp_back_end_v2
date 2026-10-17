package database

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/evidencesourceprojection"
)

// cohortCacheFixture serves the guarded disposable fixture's brand pages from
// held guard-free cohorts and compares every page with the executable SQL
// oracle. Fixtures share it so each existing parity matrix also proves the
// cached path. A held cohort is dropped only by reset(); nothing else refreshes
// it, so publication, ownership, status and privacy changes made between
// checks must be live from the blocking sets alone.
type cohortCacheFixture struct {
	d *Database
}

// Print the complete synthetic public fixture fields, including pointer-backed
// evidence and recurrence. Struct formatting otherwise hides the differing
// field behind pointer addresses when real MySQL parity fails.
func cohortParityDiagnostic(got, want EvidenceBrandPage) string {
	gotJSON, gotErr := json.MarshalIndent(got, "", "  ")
	wantJSON, wantErr := json.MarshalIndent(want, "", "  ")
	return fmt.Sprintf("got JSON (marshal error %v):\n%s\nwant JSON (marshal error %v):\n%s", gotErr, gotJSON, wantErr, wantJSON)
}

func newCohortCacheFixture(t *testing.T, db *sql.DB) *cohortCacheFixture {
	t.Helper()
	d := &Database{db: db}
	d.evidenceCohorts.configure(evidenceCohortCacheConfig{enabled: true, interval: time.Hour, budget: time.Minute, maxKeys: 64})
	t.Cleanup(d.StopEvidenceBrandCohorts)
	return &cohortCacheFixture{d: d}
}

// reset drops every held cohort. Fixtures call it only after changing
// refresh-time cohort data: brand membership or analyses, public identity, or
// the source projections and their parents.
func (c *cohortCacheFixture) reset() {
	cache := c.d.evidenceCohortCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.entries = map[string]*list.Element{}
	cache.recent.Init()
}

// snapshotAt is the held cohort's snapshot time for the filter's scope.
func (c *cohortCacheFixture) snapshotAt(t *testing.T, f EvidenceFilter) time.Time {
	t.Helper()
	f, err := NormalizeEvidenceFilter(f)
	if err != nil {
		t.Fatal(err)
	}
	cache := c.d.evidenceCohortCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, held := cache.entries[evidenceCohortKey(f)]
	if !held || element.Value.(*evidenceCohortEntry).cohort == nil {
		t.Fatalf("no cohort is held for %s/%s", f.BrandName, f.Language)
	}
	return element.Value.(*evidenceCohortEntry).cohort.snapshotAt
}

// parity compares the completed-normalizer state with the SQL oracle. Pending
// generations have a deliberately more conservative cached privacy policy and
// are tested separately below, without running the normalizer first. Completing
// source generations here never refreshes the held cohort or its live guards.
func (c *cohortCacheFixture) parity(t *testing.T, f EvidenceFilter) EvidenceBrandPage {
	t.Helper()
	f, err := NormalizeEvidenceFilter(f)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	args := make([]any, len(f.Brands))
	for i, brand := range f.Brands {
		args[i] = brand
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := c.d.db.QueryContext(ctx, `SELECT DISTINCT p.seq FROM report_evidence_source_projection p JOIN reports r ON r.seq=p.seq JOIN report_analysis ra ON ra.seq=p.seq WHERE p.dirty=TRUE AND ra.brand_name IN (`+marks+`)`, args...)
	if err != nil {
		t.Fatal(err)
	}
	dirty := []int{}
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		dirty = append(dirty, seq)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(dirty) > 0 {
		if _, err := RefreshEvidenceSourceProjection(ctx, c.d.db, dirty); err != nil {
			t.Fatalf("complete source generations before oracle parity: %v", err)
		}
	}
	cache := c.d.evidenceCohortCache()
	cache.mu.Lock()
	element, held := cache.entries[evidenceCohortKey(f)]
	loaded := held && element.Value.(*evidenceCohortEntry).cohort != nil
	cache.mu.Unlock()
	if !loaded {
		if err := cache.refreshNow(ctx, f); err != nil {
			t.Fatalf("cohort load %s/%s: %v", f.BrandName, f.Language, err)
		}
	}
	got, err := c.d.getEvidenceBrandPageProjected(ctx, f)
	if err != nil {
		t.Fatalf("cached brand page %s/%s/%s/%s/%d: %v", f.BrandName, f.Language, f.Kind, f.Sort, f.Offset, err)
	}
	if got.SnapshotAt == nil {
		t.Fatalf("page %s/%s was not served from the held cohort", f.BrandName, f.Language)
	}
	got.SnapshotAt = nil
	want, err := c.d.getEvidenceBrandPageSQL(ctx, f)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cached cohort page differs from SQL oracle (%s/%s/%s/%s/%d): %v\n%s", f.BrandName, f.Language, f.Kind, f.Sort, f.Offset, err, cohortParityDiagnostic(got, want))
	}
	return got
}

// Runs only inside TestEvidenceMySQLFixture's guarded disposable database.
// Seeds stay outside 6900–6999, where ProjectedOptionalOwnerIndex leaves
// unrelated private owners (6900–6963) behind in reports_owners.
func testProjectedEvidenceCohortCacheMySQL(t *testing.T, db *sql.DB) {
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	const first, last = 7200, 7217
	t.Run("NativeFloatDisplayIdentity", func(t *testing.T) {
		// FLOAT storage is widened when the cohort scans it. JSON_TABLE must
		// restore that native FLOAT identity before exact comparison: its DOUBLE
		// conversion can otherwise lose one bit during the JSON round trip.
		const seq = 7218
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,'native_float','rpt_native_float','2026-10-15',12.25,-41.5,'')`, seq)
		for _, row := range []struct {
			title    string
			severity any
		}{
			{"native_091", float64(float32(0.91))},
			{"adjacent_float", float64(math.Nextafter32(float32(0.91), float32(math.Inf(1))))},
			{"null_float", nil},
		} {
			exec(`INSERT INTO report_analysis VALUES(?,'en','nativefloat','Native Float',TRUE,'digital','2026-10-15',?,'Matching tuple',?)`, seq, row.title, row.severity)
		}
		for _, title := range []string{"native_091", "adjacent_float", "null_float"} {
			var severity sql.NullFloat64
			if err := db.QueryRow(`SELECT CAST(severity_level AS DOUBLE) FROM report_analysis WHERE seq=? AND title=?`, seq, title).Scan(&severity); err != nil {
				t.Fatal(err)
			}
			if severity.Valid {
				payload, _ := json.Marshal(map[string]float64{"severity": severity.Float64})
				var widened, native float64
				if err := db.QueryRow(`SELECT CAST(s.double_value AS DOUBLE),CAST(s.float_value AS DOUBLE) FROM JSON_TABLE(?, '$' COLUMNS(double_value DOUBLE PATH '$.severity',float_value FLOAT PATH '$.severity')) s`, string(payload)).Scan(&widened, &native); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s native=%.17g JSON_DOUBLE=%.17g JSON_FLOAT=%.17g", title, severity.Float64, widened, native)
				if native != severity.Float64 {
					t.Fatal("JSON FLOAT did not preserve the stored severity identity")
				}
			}
			created := "2026-10-15 00:00:00.000000"
			chosen := map[int]evidenceAnalysisCore{seq: {Record: evidenceRecord{Seq: seq, Language: "en", Brand: "nativefloat", Classification: "digital", Severity: severity.Float64, SeverityNull: !severity.Valid}, CreatedLiteral: &created}}
			grouped := evidenceGroupedPage{Records: []evidenceRecord{{Seq: seq}}, SupportingSamples: map[int][]evidenceRecord{}}
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if err != nil {
				t.Fatal(err)
			}
			err = hydrateEvidenceDisplay(context.Background(), tx, EvidenceFilter{Language: "en"}, chosen, &grouped)
			_ = tx.Rollback()
			if err != nil || grouped.Records[0].Title != title {
				t.Fatalf("native nullable FLOAT selection changed for %s at %.17g (valid=%v): title=%q err=%v", title, severity.Float64, severity.Valid, grouped.Records[0].Title, err)
			}
		}
	})
	ids := []int{}
	for seq := first; seq <= last; seq++ {
		ids = append(ids, seq)
		day := 1 + (seq-first)%15
		exec(`INSERT INTO reports(seq,id,public_id,ts,latitude,longitude,image) VALUES(?,?,?,?,12.25,-41.5,'')`, seq, fmt.Sprint("cohort_", seq), fmt.Sprint("rpt_cohort_", seq), fmt.Sprintf("2026-10-%02d", day))
		severity := 0.95 - float64(seq-first)*0.02
		exec(`INSERT INTO report_analysis VALUES(?,'en','cohortcache','Cohort Cache',TRUE,'digital','2026-10-15',?,?,?)`, seq, fmt.Sprint("Cohort ", seq), fmt.Sprint("Summary ", seq), severity)
		if seq < first+10 {
			exec(`INSERT INTO report_analysis VALUES(?,'fr','cohortcache','Cohorte',TRUE,'digital','2026-10-14',?,?,?)`, seq, fmt.Sprint("Cohorte ", seq), fmt.Sprint("Resume ", seq), severity-0.1)
		}
		kind, relationship := "firsthand", "affected"
		if seq == first+14 {
			kind, relationship = "coverage", "mentioned"
		}
		// first+15 has no evidence row and therefore remains unknown.
		if seq != first+15 {
			exec(`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES(?,?,'Observed','Blocked',?,?,1)`, seq, kind, fmt.Sprint("Reason ", seq), relationship)
		}
		url := fmt.Sprintf("https://example.test/cohort/%d", seq)
		if seq == last {
			url = fmt.Sprintf("https://example.test/cohort/%d", first+13) // duplicate import of one source
		}
		exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url,source_timestamp,created_at) VALUES(?,'custom',?,?,?,'2026-10-15')`, seq, fmt.Sprint("cohort_", seq), url, fmt.Sprintf("2026-10-%02d 08:00:00", day))
		if seq <= first+9 {
			exec(`INSERT INTO case_reports VALUES(?,'cohort_cache_family_a')`, seq)
		} else if seq <= first+12 {
			exec(`INSERT INTO case_reports VALUES(?,'cohort_cache_family_b')`, seq)
		}
	}
	// Ten-member family A exercises first-eight-then-privacy sampling with one
	// privacy-restricted member inside the first eight.
	exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,report_json,provenance_json,observed_at,submitted_at,extensions_json) VALUES('cohort_private',?,JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)),JSON_OBJECT(),NULL,'2026-10-15',JSON_OBJECT())`, first+3)
	clock := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	cached := newCohortCacheFixture(t, db)
	matrix := func(t *testing.T) {
		t.Helper()
		for _, language := range []string{"en", "fr"} {
			for _, kind := range []string{"firsthand", "coverage", "unknown", "all"} {
				for _, sortBy := range []string{"recent", "signal", "recurrence"} {
					for _, offset := range []int{0, 2, 100} {
						cached.parity(t, EvidenceFilter{BrandName: "cohortcache", Language: language, Kind: kind, Sort: sortBy, Limit: 2, Offset: offset, asOf: clock})
					}
				}
			}
		}
	}
	all := EvidenceFilter{BrandName: "cohortcache", Language: "en", Kind: "all", Sort: "signal", Limit: 50, asOf: clock}
	for _, mode := range []string{"cold", "warm"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "warm" {
				result, err := RefreshEvidenceSourceProjection(ctx, db, ids)
				if err != nil || result.Computed != len(ids) || result.MissingParents != 0 {
					t.Fatalf("source projection refresh failed: %+v %v", result, err)
				}
				cached.reset() // source projections are refresh-time cohort data
			}
			matrix(t)
			got := cached.parity(t, all)
			// Families A and B, the deduplicated first+13/last source, first+16,
			// one coverage report and one unknown report.
			if got.Overview.TotalReports != 18 || got.KindCounts.All != 6 || got.KindCounts.Firsthand != 4 || got.KindCounts.Coverage != 1 || got.KindCounts.Unknown != 1 {
				t.Fatalf("families, source dedup or facets changed: %+v", got)
			}
		})
	}
	t.Run("LiveGuardsWithoutRefresh", func(t *testing.T) {
		f := EvidenceFilter{BrandName: "cohortcache", Language: "en", Kind: "all", Sort: "signal", Limit: 3, asOf: clock}
		before := cached.parity(t, f)
		snapshot := cached.snapshotAt(t, f)
		if len(before.Reports) != 3 {
			t.Fatalf("cached page before publication changes: %+v", before)
		}
		representative, other := before.Reports[0].Report.Seq, before.Reports[1].Report.Seq
		if representative < first || representative > first+9 || other < first+10 || other > first+12 || len(before.Reports[1].Recurrence.SupportingSources) != 3 {
			t.Fatalf("signal order or family samples changed: %+v", before.Reports)
		}
		member := first + 10
		if member == other {
			member = first + 11
		}
		// One change per guard: a private owner on family A's representative,
		// an inactive status on a family B member, a shadowed singleton, and a
		// privacy restriction on a clean family A member that the projection
		// worker's path recomputes. None of them refreshes the cohort.
		exec(`INSERT INTO reports_owners VALUES(?,'late-private-owner',FALSE)`, representative)
		exec(`INSERT INTO report_status VALUES(?,'resolved')`, member)
		exec(`INSERT INTO report_raw(report_seq,visibility) VALUES(?,'shadow')`, first+16)
		exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,report_json,provenance_json,observed_at,submitted_at,extensions_json) VALUES('cohort_late_private',?,JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)),JSON_OBJECT(),NULL,'2026-10-15',JSON_OBJECT())`, first+8)
		if result, err := RefreshEvidenceSourceProjection(ctx, db, []int{first + 8}); err != nil || result.Computed != 1 {
			t.Fatalf("worker-path projection refresh failed: %+v %v", result, err)
		}
		after := cached.parity(t, f)
		if !cached.snapshotAt(t, f).Equal(snapshot) {
			t.Fatal("the cohort was refreshed")
		}
		if after.Overview.TotalReports != before.Overview.TotalReports-3 || after.Reports[0].Report.Seq == representative || after.Reports[1].Recurrence.SupportingReportCount != 2 {
			t.Fatalf("private representative, inactive member or shadowed singleton stayed in the live page: %+v", after)
		}
		for _, sample := range after.Reports[0].Recurrence.SupportingSources {
			if sample.Title == fmt.Sprint("Cohort ", first+8) {
				t.Fatal("a report restricted after the snapshot kept its public sample")
			}
		}
		matrix(t)
		exec(`UPDATE reports_owners SET is_public=TRUE WHERE seq=?`, representative)
		exec(`DELETE FROM report_status WHERE seq=? AND status='resolved'`, member)
		exec(`DELETE FROM report_raw WHERE report_seq=?`, first+16)
		exec(`DELETE FROM wire_submissions_raw WHERE submission_id='cohort_late_private'`)
		if result, err := RefreshEvidenceSourceProjection(ctx, db, []int{first + 8}); err != nil || result.Computed != 1 {
			t.Fatalf("worker-path projection refresh failed: %+v %v", result, err)
		}
		restored := cached.parity(t, f)
		if !cached.snapshotAt(t, f).Equal(snapshot) || !reflect.DeepEqual(restored, before) {
			t.Fatalf("reverting live guards did not restore the page without a refresh: %+v != %+v", restored, before)
		}
		matrix(t)
	})
	t.Run("DirtyPrivacyBeforeWorkerWithoutCohortRefresh", func(t *testing.T) {
		f, _ := NormalizeEvidenceFilter(all)
		before := cached.parity(t, f)
		snapshot := cached.snapshotAt(t, f)
		const sampleSeq = first + 2
		publicSample, publicStandalone := false, false
		for _, report := range before.Reports {
			if report.Report.Seq == first+16 && report.Evidence.SourceURL != nil && report.Evidence.Excerpt != nil {
				publicStandalone = true
			}
			for _, sample := range report.Recurrence.SupportingSources {
				if sample.Title == fmt.Sprint("Cohort ", sampleSeq) && sample.SourceURL != nil {
					publicSample = true
				}
			}
		}
		if !publicSample || !publicStandalone {
			t.Fatalf("precondition: pending privacy must target an exposed sample and standalone source, sample=%v standalone=%v", publicSample, publicStandalone)
		}
		// Mutate both a standalone page report and an existing family sample.
		// The projection trigger only marks dirty; its old privacy flag is false.
		for _, seq := range []int{first + 16, sampleSeq} {
			exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,report_json,provenance_json,observed_at,submitted_at,extensions_json) VALUES(?,?,JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)),JSON_OBJECT(),NULL,'2026-10-15',JSON_OBJECT())`, fmt.Sprint("pending_privacy_", seq), seq)
			var dirty, restricted bool
			if err := db.QueryRow(`SELECT dirty,privacy_restricted FROM report_evidence_source_projection WHERE seq=?`, seq).Scan(&dirty, &restricted); err != nil || !dirty || restricted {
				t.Fatalf("fixture must expose pending redaction before worker recomputation: dirty=%v restricted=%v err=%v", dirty, restricted, err)
			}
		}
		// Deliberately do not use parity(): it completes dirty generations first.
		after, err := cached.d.getEvidenceBrandPageProjected(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if after.SnapshotAt == nil || !after.SnapshotAt.Equal(snapshot) || after.KindCounts != before.KindCounts || after.Total != before.Total || after.Overview.TotalReports != before.Overview.TotalReports {
			t.Fatalf("pending source privacy changed cached membership/counts or reloaded the cohort: %+v", after)
		}
		found := false
		for _, report := range after.Reports {
			if report.Report.Seq == first+16 {
				found = true
				if report.Report.SourceURL != nil || report.Evidence.SourceURL != nil || report.Evidence.Excerpt != nil || report.Evidence.ReportedImpact != nil || report.Evidence.ClassificationReason != nil || report.Evidence.HasOriginalMedia || len(report.Recurrence.SupportingSources) != 0 {
					t.Fatalf("pending privacy restriction exposed original source fields: %+v", report)
				}
			}
			for _, sample := range report.Recurrence.SupportingSources {
				if sample.Title == fmt.Sprint("Cohort ", sampleSeq) || (sample.SourceURL != nil && *sample.SourceURL == fmt.Sprintf("https://example.test/cohort/%d", sampleSeq)) {
					t.Fatal("dirty privacy generation remained in public supporting samples")
				}
			}
		}
		if !found {
			t.Fatal("restricted standalone report disappeared instead of retaining its count")
		}
		// Once the worker completes, the held cohort remains safe and the full
		// exact SQL parity contract applies again, without refreshing that cohort.
		cached.parity(t, f)
		for _, seq := range []int{first + 16, sampleSeq} {
			exec(`DELETE FROM wire_submissions_raw WHERE submission_id=?`, fmt.Sprint("pending_privacy_", seq))
		}
		restored := cached.parity(t, f)
		if !reflect.DeepEqual(restored, before) || !cached.snapshotAt(t, f).Equal(snapshot) {
			t.Fatal("normalizer completion did not restore the original cached page")
		}
	})
	t.Run("PrivacyIndexMigration", func(t *testing.T) {
		privacyIndexes := func() int {
			t.Helper()
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?`, evidencesourceprojection.Table, evidencesourceprojection.PrivacyRestrictedIndex).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		if privacyIndexes() != 1 {
			t.Fatal("fresh projection table lacks the privacy index")
		}
		if err := evidencesourceprojection.Verify(ctx, db); err != nil {
			t.Fatal(err)
		}
		// A table created before the index verifies and serves identically: the
		// hint only warns, and the blocklist is the same set.
		exec(`ALTER TABLE ` + evidencesourceprojection.Table + ` DROP INDEX ` + evidencesourceprojection.PrivacyRestrictedIndex)
		if err := evidencesourceprojection.Verify(ctx, db); err != nil {
			t.Fatalf("verify rejected a projection table without the optional index: %v", err)
		}
		cached.parity(t, all)
		for attempt := 0; attempt < 2; attempt++ {
			if err := evidencesourceprojection.MigratePrivacyRestrictedIndex(ctx, db); err != nil {
				t.Fatalf("privacy index migration attempt %d: %v", attempt+1, err)
			}
		}
		if privacyIndexes() != 1 {
			t.Fatal("online privacy index migration did not add exactly one index")
		}
		if err := evidencesourceprojection.Verify(ctx, db); err != nil {
			t.Fatal(err)
		}
		cached.parity(t, all)
	})
	t.Run("Disabled", func(t *testing.T) {
		direct := &Database{db: db}
		direct.evidenceCohorts.configure(evidenceCohortCacheConfig{})
		f, _ := NormalizeEvidenceFilter(all)
		if _, ok := direct.evidenceCohortCache().lookup(f); ok {
			t.Fatal("disabled cache served a cohort")
		}
		for _, kind := range []string{"firsthand", "all"} {
			for _, offset := range []int{0, 2} {
				f := EvidenceFilter{BrandName: "cohortcache", Language: "en", Kind: kind, Sort: "signal", Limit: 2, Offset: offset, asOf: clock}
				got, err := direct.getEvidenceBrandPageProjected(ctx, f)
				if err != nil || got.SnapshotAt != nil {
					t.Fatalf("direct path changed or reported a snapshot: %v %+v", err, got.SnapshotAt)
				}
				want, err := direct.getEvidenceBrandPageSQL(ctx, f)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("direct page differs from SQL oracle: %v\n%s", err, cohortParityDiagnostic(got, want))
				}
			}
		}
	})
}
