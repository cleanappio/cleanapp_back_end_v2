package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cleanapp-common/evidencesourceprojection"
	"github.com/DATA-DOG/go-sqlmock"
)

func cohortTestRecord(seq int) evidenceRecord {
	published := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	return evidenceRecord{Seq: seq, Classification: "digital", Severity: 0.7, Language: "fr", Brand: "claude", Kind: "firsthand", Excerpt: "1", Impact: "1", Relationship: "affected", Version: 1, PublishedAt: &published, IngestedAtNull: true, SourceKey: fmt.Sprintf("url:example.test/report/%d", seq)}
}

// Three firsthand reports of a guard-free cohort: 42 and 43 share a case
// family (43 wins), 44 stands alone. Only grouping inputs are present, as in a
// loaded cohort; no publication state is held.
func cohortTestSnapshot(f EvidenceFilter) evidenceCohort {
	cohort := evidenceCohort{key: evidenceCohortKey(f), snapshotAt: time.Now().UTC().Truncate(time.Microsecond), bySeq: map[int]int{}, freshSources: map[int]bool{}, cases: map[int]evidenceCaseReference{42: {ID: "family-1"}, 43: {ID: "family-1"}}}
	for i, seq := range []int{42, 43, 44} {
		cohort.records = append(cohort.records, cohortTestRecord(seq))
		cohort.selections = append(cohort.selections, evidenceAnalysisSelection{created: sql.NullString{String: "2026-10-15 00:00:00.000000", Valid: true}, requested: sql.NullInt64{Int64: 1, Valid: true}, english: sql.NullInt64{Valid: true}, languageWeight: []byte("fr"), createdTime: sql.NullTime{Time: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), Valid: true}, sourceClean: true})
		cohort.bySeq[seq] = i
	}
	return cohort
}

// Existing unit fixtures advance fake clocks explicitly. Suppress uncontrolled
// wall-clock ticks and always stop the cache-owned maintenance goroutine.
func cohortTestCache(t *testing.T, d *Database) *evidenceCohortCache {
	t.Helper()
	if d.evidenceCohorts.maintenanceTicks == nil {
		d.evidenceCohorts.maintenanceTicks = make(chan time.Time)
	}
	c := d.evidenceCohortCache()
	t.Cleanup(c.stop)
	return c
}

func cohortTestConfig() evidenceCohortCacheConfig {
	return evidenceCohortCacheConfig{enabled: true, interval: time.Minute, budget: time.Minute, maxKeys: 8}
}

func TestEvidenceCohortKeyIsCanonicalForScopeAndLanguageOnly(t *testing.T) {
	base, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Language: "fr", Kind: "firsthand", Sort: "recent", Limit: 12})
	paged, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Language: "fr", Kind: "all", Sort: "recurrence", Limit: 50, Offset: 100})
	if evidenceCohortKey(base) != evidenceCohortKey(paged) {
		t.Fatal("kind/sort/limit/offset changed the cohort key")
	}
	reversed := base
	reversed.Brands = append([]string(nil), base.Brands...)
	for i, j := 0, len(reversed.Brands)-1; i < j; i, j = i+1, j-1 {
		reversed.Brands[i], reversed.Brands[j] = reversed.Brands[j], reversed.Brands[i]
	}
	if evidenceCohortKey(reversed) != evidenceCohortKey(base) {
		t.Fatal("brand order changed the cohort key")
	}
	english, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Language: "en"})
	product, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Product: "claude", Language: "fr"})
	if evidenceCohortKey(english) == evidenceCohortKey(base) || evidenceCohortKey(product) == evidenceCohortKey(base) {
		t.Fatal("language or product scope did not separate cohort keys")
	}
}

func TestEvidenceCohortCacheIsDisabledByDefault(t *testing.T) {
	var calls int32
	d := &Database{}
	d.evidenceCohorts.load = func(context.Context, EvidenceFilter, time.Duration) (evidenceCohort, error) {
		atomic.AddInt32(&calls, 1)
		return evidenceCohort{}, nil
	}
	c := cohortTestCache(t, d)
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude"})
	if _, ok := c.lookup(f); ok || c.config.enabled {
		t.Fatal("cohort cache served without opt-in")
	}
	c.inflight.Wait()
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("disabled cohort cache scheduled a refresh")
	}
}

func TestEvidenceCohortCacheFloorsDegenerateConfiguration(t *testing.T) {
	d := &Database{}
	d.evidenceCohorts.configure(evidenceCohortCacheConfig{enabled: true})
	c := cohortTestCache(t, d)
	if c.config.interval != time.Second || c.config.budget != time.Second || c.config.maxKeys != 1 {
		t.Fatalf("zero interval/budget/key bound were not floored: %+v", c.config)
	}
}

// The refresh statement is the direct statement minus its live guard block,
// minus the report_raw join only that block used, under its own budget. Brand
// membership, the non-empty public identity, evidence bits and the source tuple
// (including the projection's privacy flag, which the live set overrides) stay
// cohort data.
func TestEvidenceCohortRefreshQueryIsTheDirectQueryWithoutLiveGuards(t *testing.T) {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Language: "fr", Kind: "all", Sort: "recurrence", Limit: 50, Offset: 100})
	direct, directArgs := projectedEvidenceBaseQuery(f)
	cohort, cohortArgs := projectedEvidenceCohortQuery(f, 60*time.Second)
	if !reflect.DeepEqual(directArgs, cohortArgs) {
		t.Fatal("refresh statement changed the scope arguments")
	}
	for _, live := range []string{PublicVisibilityWhereSQL, "report_raw", "report_status", "reports_owners", "NOT EXISTS", "COUNT(*)", "JOIN_INDEX"} {
		if strings.Contains(cohort, live) {
			t.Fatalf("refresh statement applies a live publication guard: %s", live)
		}
	}
	for _, kept := range []string{"COLLATE utf8mb4_bin)<>''", "ra.classification='digital' AND ra.is_valid=TRUE", "FORCE INDEX (" + evidenceBrandCoreIndex + ")", "MAX_EXECUTION_TIME(60000)", evidencesourceprojection.CleanPredicate("p"), "LEFT JOIN report_evidence ev", "COALESCE(p.privacy_restricted,FALSE)", "WHERE ra.seq IS NOT NULL"} {
		if !strings.Contains(cohort, kept) {
			t.Fatalf("refresh statement lost refresh-time cohort data: %s", kept)
		}
	}
	guardBlock := strings.Index(direct, " AND "+PublicVisibilityWhereSQL)
	if guardBlock < 0 || !strings.Contains(direct, "MAX_EXECUTION_TIME(8000)") {
		t.Fatal("direct statement lost its live guard block or request budget")
	}
	stripped := strings.Replace(direct[:guardBlock], "\n LEFT JOIN report_raw rr ON rr.report_seq=ra.seq", "", 1)
	stripped = strings.Replace(stripped, "MAX_EXECUTION_TIME(8000)", "MAX_EXECUTION_TIME(60000)", 1)
	if stripped != cohort {
		t.Fatalf("refresh statement differs from the direct statement beyond guards, raw join and budget:\n%s\n---\n%s", stripped, cohort)
	}
}

func TestPublicationBlocklistQueriesAreExactGuardComplementsOverCoveringIndexes(t *testing.T) {
	// The second complement below negates this predicate verbatim.
	if PublicVisibilityWhereSQL != "(rr.visibility IS NULL OR rr.visibility = 'public')" {
		t.Fatal("public visibility predicate changed; realign the raw blocklist complement")
	}
	want := [5]string{
		"SELECT /*+ MAX_EXECUTION_TIME(8000) INDEX(rs idx_rs_seq_status) */ rs.seq FROM report_status rs WHERE rs.seq IS NOT NULL AND rs.status IS NOT NULL AND rs.status<>'active'",
		"SELECT /*+ MAX_EXECUTION_TIME(8000) INDEX(rr idx_visibility) */ rr.report_seq FROM report_raw rr WHERE rr.visibility IS NOT NULL AND rr.visibility<>'public'",
		"SELECT /*+ MAX_EXECUTION_TIME(8000) INDEX(ro idx_is_public) */ ro.seq FROM reports_owners ro WHERE ro.is_public=FALSE AND ro.seq IS NOT NULL AND ro.owner IS NOT NULL AND ro.owner<>''",
		"SELECT /*+ MAX_EXECUTION_TIME(8000) INDEX(p idx_privacy_restricted) */ p.seq FROM report_evidence_source_projection p WHERE p.privacy_restricted=TRUE",
		"SELECT /*+ MAX_EXECUTION_TIME(8000) INDEX(p idx_dirty_seq) */ p.seq FROM report_evidence_source_projection p WHERE p.dirty=TRUE",
	}
	if got := publicationBlocklistQueries(); got != want {
		t.Fatalf("blocklist statements changed:\n%q", got)
	}
	// The direct guards still compare the same columns and literals the
	// complements negate, so both paths agree under the columns' collations.
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft"})
	direct, _ := projectedEvidenceBaseQuery(f)
	for _, predicate := range []string{"rs.status IS NOT NULL AND rs.status<>'active'", "ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE", "COALESCE(p.privacy_restricted,FALSE)"} {
		if !strings.Contains(direct, predicate) {
			t.Fatalf("direct guard predicate changed; realign its blocklist complement: %s", predicate)
		}
	}
	for _, query := range want {
		if strings.Contains(query, "?") || strings.Contains(query, "JSON_TABLE") || strings.Contains(query, "JOIN ") || strings.Contains(query, "FORCE INDEX") {
			t.Fatal("a blocklist read became per-report, joined, or dependent on an index's presence")
		}
	}
}

func TestLoadPublicationBlocklistsReadsFiveSetsInOrderAndFailsClosed(t *testing.T) {
	queries := publicationBlocklistQueries()
	t.Run("complete", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(queries[0])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(8).AddRow(8).AddRow(9))
		mock.ExpectQuery(regexp.QuoteMeta(queries[1])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(6))
		mock.ExpectQuery(regexp.QuoteMeta(queries[2])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(7))
		mock.ExpectQuery(regexp.QuoteMeta(queries[3])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(29))
		mock.ExpectQuery(regexp.QuoteMeta(queries[4])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(30))
		mock.ExpectRollback()
		tx, _ := db.Begin()
		b, err := loadPublicationBlocklists(context.Background(), tx)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.inactiveStatus) != 2 || !b.blocked(8) || !b.blocked(9) || !b.blocked(6) || !b.blocked(7) || b.blocked(29) || b.blocked(1) {
			t.Fatalf("blocking sets changed: %+v", b)
		}
		if _, restricted := b.privacyRestricted[29]; !restricted || len(b.privacyRestricted) != 1 {
			t.Fatal("privacy set changed")
		}
		if _, dirty := b.dirtySources[30]; !dirty || len(b.dirtySources) != 1 {
			t.Fatal("dirty source set changed")
		}
		_ = tx.Rollback()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("partial", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(queries[0])).WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(8))
		mock.ExpectQuery(regexp.QuoteMeta(queries[1])).WillReturnRows(sqlmock.NewRows([]string{"seq"}))
		mock.ExpectQuery(regexp.QuoteMeta(queries[2])).WillReturnError(errors.New("deadline"))
		mock.ExpectRollback()
		tx, _ := db.Begin()
		b, err := loadPublicationBlocklists(context.Background(), tx)
		if err == nil || b.inactiveStatus != nil || b.nonPublicRaw != nil {
			t.Fatal("a partial blocklist read was applied")
		}
		_ = tx.Rollback()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestApplyPublicationBlocklistsDropsGuardedReportsAndTakesLivePrivacy(t *testing.T) {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Language: "fr"})
	cohort := cohortTestSnapshot(f)
	cohort.records = append(cohort.records, cohortTestRecord(45), cohortTestRecord(46))
	cohort.records[2].PrivacyRestricted = true // 44: fresh at refresh, restricted then
	cohort.records[3].PrivacyRestricted = true // 45: clean at refresh, restricted then
	cohort.freshSources[44] = true
	live := applyPublicationBlocklists(cohort, publicationBlocklists{inactiveStatus: map[int]struct{}{42: {}}, nonPublicRaw: map[int]struct{}{}, privateOwner: map[int]struct{}{46: {}}, privacyRestricted: map[int]struct{}{43: {}}})
	got := map[int]bool{}
	for _, r := range live {
		got[r.Seq] = r.PrivacyRestricted
	}
	// 42 inactive and 46 private are gone; 43 is restricted live; 44 keeps its
	// exact fresh restriction (its live column is an older generation); 45's
	// restriction was lifted by a newer generation.
	if !reflect.DeepEqual(got, map[int]bool{43: true, 44: true, 45: false}) {
		t.Fatalf("live subset changed: %v", got)
	}
	if len(cohort.records) != 5 || cohort.records[1].PrivacyRestricted || !cohort.records[3].PrivacyRestricted {
		t.Fatal("shared cohort was mutated")
	}
}

func TestDirtyPrivacyIsAppliedBeforeSourceWinnerAndCategoryCounts(t *testing.T) {
	firsthand, coverage := cohortTestRecord(44), cohortTestRecord(43)
	coverage.SourceKey = firsthand.SourceKey
	coverage.Kind = "coverage"
	cohort := evidenceCohort{records: []evidenceRecord{firsthand, coverage}}
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
	before := groupEvidenceRecords(cohort.records, nil, f)
	live := applyPublicationBlocklists(cohort, publicationBlocklists{dirtySources: map[int]struct{}{44: {}}})
	after := groupEvidenceRecords(live, nil, f)
	if before.KindCounts.Firsthand != 1 || before.KindCounts.Coverage != 0 || after.KindCounts.Firsthand != 0 || after.KindCounts.Coverage != 1 || after.KindCounts.All != 1 || after.Total != 1 || after.Records[0].Seq != 43 {
		t.Fatalf("dirty privacy did not promote the public source winner before exact category counting: before=%+v after=%+v", before, after)
	}
	if cohort.records[0].PrivacyRestricted {
		t.Fatal("request-local privacy changed the held cohort")
	}
}

func TestEvidenceCohortCacheServesLiveGuardedPageFromHeldCohort(t *testing.T) {
	type mode struct {
		name                    string
		sets                    [5][]int
		restrictedAtRefresh     []int
		fresh                   []int
		dirtyOutput             []int
		hydrated                []int
		pageIDs                 string
		reports                 []int
		firstSamples, overview  int
		total                   int
		firstSupportingCount    int
		secondSourceURLWithheld bool
	}
	modes := []mode{
		{name: "unblocked", hydrated: []int{43, 42, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 2, overview: 3, total: 2, firstSupportingCount: 2},
		{name: "representative_inactive", sets: [5][]int{{43}}, hydrated: []int{44, 42}, pageIDs: "[44,42]", reports: []int{44, 42}, firstSamples: 1, overview: 2, total: 2, firstSupportingCount: 1},
		// With 42 gone, 43 and 44 are tied singleton families and sort by seq.
		{name: "sample_private", sets: [5][]int{nil, nil, {42}}, hydrated: []int{44, 43}, pageIDs: "[44,43]", reports: []int{44, 43}, firstSamples: 1, overview: 2, total: 2, firstSupportingCount: 1},
		{name: "singleton_shadow", sets: [5][]int{nil, {44}}, hydrated: []int{43, 42}, pageIDs: "[43]", reports: []int{43}, firstSamples: 2, overview: 2, total: 1, firstSupportingCount: 2},
		{name: "sample_restricted_live", sets: [5][]int{nil, nil, nil, {42}}, hydrated: []int{43, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 1, overview: 3, total: 2, firstSupportingCount: 2},
		{name: "fresh_restriction_kept", restrictedAtRefresh: []int{44}, fresh: []int{44}, hydrated: []int{43, 42, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 2, overview: 3, total: 2, firstSupportingCount: 2, secondSourceURLWithheld: true},
		{name: "clean_restriction_lifted", restrictedAtRefresh: []int{42}, hydrated: []int{43, 42, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 2, overview: 3, total: 2, firstSupportingCount: 2},
		{name: "sample_dirty_after_refresh", sets: [5][]int{nil, nil, nil, nil, {42}}, hydrated: []int{43, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 1, overview: 3, total: 2, firstSupportingCount: 2},
		{name: "singleton_dirty_after_refresh", sets: [5][]int{nil, nil, nil, nil, {44}}, dirtyOutput: []int{44}, hydrated: []int{43, 42, 44}, pageIDs: "[43,44]", reports: []int{43, 44}, firstSamples: 2, overview: 3, total: 2, firstSupportingCount: 2, secondSourceURLWithheld: true},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 2, asOf: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)})
			snapshot := cohortTestSnapshot(f)
			if m.secondSourceURLWithheld {
				snapshot.records[snapshot.bySeq[44]].HasOriginalMedia = true
			}
			for _, seq := range m.restrictedAtRefresh {
				snapshot.records[snapshot.bySeq[seq]].PrivacyRestricted = true
			}
			for _, seq := range m.fresh {
				snapshot.freshSources[seq] = true
			}
			var loads int32
			d := &Database{db: db}
			d.evidenceCohorts.configure(cohortTestConfig())
			d.evidenceCohorts.load = func(context.Context, EvidenceFilter, time.Duration) (evidenceCohort, error) {
				atomic.AddInt32(&loads, 1)
				return snapshot, nil
			}
			c := cohortTestCache(t, d)
			if err := c.refreshNow(context.Background(), f); err != nil {
				t.Fatal(err)
			}
			mock.ExpectBegin()
			// The five live sets precede any grouping or hydration; they take no
			// per-report arguments.
			for i, query := range publicationBlocklistQueries() {
				rows := sqlmock.NewRows([]string{"seq"})
				for _, seq := range m.sets[i] {
					rows.AddRow(seq)
				}
				mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)
			}
			dirty := map[int]bool{}
			for _, seq := range m.dirtyOutput {
				dirty[seq] = true
			}
			output := sqlmock.NewRows(projectedOutputTestColumns)
			display := sqlmock.NewRows([]string{"seq", "title", "summary", "brand"})
			for _, seq := range m.hydrated {
				row := projectedOutputTestRow(seq)
				if dirty[seq] {
					row[4] = false
				}
				output.AddRow(row...)
				display.AddRow(seq, fmt.Sprint("Title ", seq), "Summary", "Claude")
			}
			mock.ExpectQuery(`SELECT ids.seq,\s+CASE`).WithArgs(evidenceIDsJSON(m.hydrated)).WillReturnRows(output)
			mock.ExpectQuery("WITH display AS").WithArgs("fr", sqlmock.AnyArg()).WillReturnRows(display)
			excerpts := sqlmock.NewRows([]string{"seq", "excerpt", "impact", "reason"})
			for _, seq := range m.reports {
				excerpts.AddRow(seq, "Observed", "Blocked", "Direct")
			}
			mock.ExpectQuery("SELECT ev.seq,COALESCE").WithArgs(m.pageIDs).WillReturnRows(excerpts)
			mock.ExpectCommit()
			page, err := d.GetEvidenceBrandPage(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			if page.SnapshotAt == nil || !page.SnapshotAt.Equal(snapshot.snapshotAt) {
				t.Fatalf("cohort snapshot time was not reported: %+v", page.SnapshotAt)
			}
			if page.Overview.TotalReports != m.overview || page.Total != m.total || page.KindCounts.All != m.total || page.KindCounts.Firsthand != m.total || page.HasMore {
				t.Fatalf("counts are not live over the guarded cohort: %+v", page)
			}
			if len(page.Reports) != len(m.reports) {
				t.Fatalf("page rows changed: %+v", page.Reports)
			}
			for i, seq := range m.reports {
				if page.Reports[i].Report.Seq != seq || page.Reports[i].Report.PublicID != fmt.Sprint("r_test_", seq) || page.Reports[i].Analysis[0].Title != fmt.Sprint("Title ", seq) {
					t.Fatalf("row %d changed or lost bounded hydration: %+v", i, page.Reports[i])
				}
			}
			first := page.Reports[0].Recurrence
			if len(first.SupportingSources) != m.firstSamples || first.SupportingReportCount != m.firstSupportingCount {
				t.Fatalf("family samples or counts are not live: %+v", first)
			}
			for _, sample := range first.SupportingSources {
				if sample.SourceURL == nil || !strings.HasPrefix(*sample.SourceURL, "https://example.test/report/") {
					t.Fatalf("sample lost its hydrated source: %+v", sample)
				}
			}
			if m.secondSourceURLWithheld {
				second := page.Reports[1]
				if second.Evidence.SourceURL != nil || second.Evidence.Excerpt != nil || second.Evidence.ReportedImpact != nil || second.Evidence.ClassificationReason != nil || second.Evidence.HasOriginalMedia || len(second.Recurrence.SupportingSources) != 0 {
					t.Fatalf("exact fresh restriction was lifted by an older live column: %+v", second)
				}
			}
			if page.Reports[0].Evidence.Excerpt == nil || *page.Reports[0].Evidence.Excerpt != "Observed" {
				t.Fatal("bounded evidence hydration changed on the cached path")
			}
			c.inflight.Wait()
			if atomic.LoadInt32(&loads) != 1 {
				t.Fatalf("serving a fresh cohort scheduled %d refreshes", loads-1)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEvidenceCohortCacheFailsClosedWhenABlocklistReadFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 2})
	snapshot := cohortTestSnapshot(f)
	d := &Database{db: db}
	d.evidenceCohorts.configure(cohortTestConfig())
	d.evidenceCohorts.load = func(context.Context, EvidenceFilter, time.Duration) (evidenceCohort, error) { return snapshot, nil }
	if err := cohortTestCache(t, d).refreshNow(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	queries := publicationBlocklistQueries()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queries[0])).WillReturnRows(sqlmock.NewRows([]string{"seq"}))
	mock.ExpectQuery(regexp.QuoteMeta(queries[1])).WillReturnError(errors.New("budget"))
	mock.ExpectRollback()
	page, err := d.GetEvidenceBrandPage(context.Background(), f)
	if err == nil || page.Total != 0 || len(page.Reports) != 0 || page.SnapshotAt != nil {
		t.Fatal("a page was served without every live blocking set")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceCohortCacheRefreshesOnceForConcurrentCallers(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	d := &Database{}
	d.evidenceCohorts.configure(cohortTestConfig())
	d.evidenceCohorts.load = func(_ context.Context, f EvidenceFilter, budget time.Duration) (evidenceCohort, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		if budget != time.Minute {
			return evidenceCohort{}, errors.New("configured budget changed")
		}
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: time.Now()}, nil
	}
	c := cohortTestCache(t, d)
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Language: "fr"})
	var callers sync.WaitGroup
	for i := 0; i < 8; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if _, ok := c.lookup(f); ok {
				t.Error("an absent cohort was served")
			}
		}()
	}
	callers.Wait()
	if err := c.refreshNow(context.Background(), f); err == nil || !strings.Contains(err.Error(), "in flight") {
		t.Fatal("synchronous refresh bypassed the in-flight refresh")
	}
	close(release)
	c.inflight.Wait()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("concurrent lookups ran %d refreshes", calls)
	}
	if _, ok := c.lookup(f); !ok || atomic.LoadInt32(&calls) != 1 {
		t.Fatal("fresh cohort was not served, or a fresh entry was refreshed again")
	}
}

func TestEvidenceCohortCacheKeepsPreviousCohortAfterFailedRefresh(t *testing.T) {
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var calls int32
	d := &Database{}
	cfg := cohortTestConfig()
	cfg.interval = 30 * time.Second
	d.evidenceCohorts.configure(cfg)
	d.evidenceCohorts.now = func() time.Time { return clock }
	d.evidenceCohorts.load = func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		if atomic.AddInt32(&calls, 1) > 1 {
			return evidenceCohort{}, errors.New("budget exceeded")
		}
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: clock}, nil
	}
	c := cohortTestCache(t, d)
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude"})
	c.lookup(f)
	c.inflight.Wait()
	first, ok := c.lookup(f)
	if !ok || !first.snapshotAt.Equal(clock) {
		t.Fatal("first cohort was not served")
	}
	clock = clock.Add(30 * time.Second)
	if cohort, ok := c.lookup(f); !ok || !cohort.snapshotAt.Equal(first.snapshotAt) {
		t.Fatal("stale cohort was not served while refreshing")
	}
	c.inflight.Wait()
	if cohort, ok := c.lookup(f); !ok || !cohort.snapshotAt.Equal(first.snapshotAt) || atomic.LoadInt32(&calls) != 2 {
		t.Fatal("failed refresh dropped the previous cohort or retried without backoff")
	}
	clock = clock.Add(59 * time.Second)
	c.lookup(f)
	c.inflight.Wait()
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatal("refresh retried before its backoff elapsed")
	}
	clock = clock.Add(2 * time.Second)
	c.lookup(f)
	c.inflight.Wait()
	if _, ok := c.lookup(f); ok || atomic.LoadInt32(&calls) != 3 {
		t.Fatal("expired snapshot was served after a backed-off refresh failure")
	}
	c.mu.Lock()
	held := c.entries[evidenceCohortKey(f)].Value.(*evidenceCohortEntry).cohort
	c.mu.Unlock()
	if held == nil || !held.snapshotAt.Equal(first.snapshotAt) {
		t.Fatal("expired cohort was not retained for refresh bookkeeping")
	}
}

func TestEvidenceCohortCacheExpiresFromSnapshotStartAtHardLimit(t *testing.T) {
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	snapshotAt := clock.Add(-time.Minute) // a refresh used its complete read budget
	d := &Database{}
	d.evidenceCohorts.configure(cohortTestConfig())
	d.evidenceCohorts.now = func() time.Time { return clock }
	var calls atomic.Int32
	d.evidenceCohorts.load = func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		if calls.Add(1) > 1 {
			return evidenceCohort{}, errors.New("refresh failed")
		}
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: snapshotAt}, nil
	}
	c := cohortTestCache(t, d)
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "google"})
	if err := c.refreshNow(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute - time.Nanosecond)
	if _, usable := c.lookup(f); !usable {
		t.Fatal("snapshot expired before interval+budget")
	}
	c.inflight.Wait()
	clock = clock.Add(time.Nanosecond)
	if _, usable := c.lookup(f); usable {
		t.Fatal("snapshot served at its hard maximum age")
	}
	c.inflight.Wait()
	clock = clock.Add(time.Hour)
	if _, usable := c.lookup(f); usable {
		t.Fatal("failed refresh extended the snapshot's hard maximum age")
	}
	c.inflight.Wait()
}

func TestEvidenceCohortCacheSerializesBackgroundLoadsAcrossKeys(t *testing.T) {
	d := &Database{}
	d.evidenceCohorts.configure(cohortTestConfig())
	started := make(chan string, 2)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	d.evidenceCohorts.load = func(ctx context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		started <- f.BrandName
		select {
		case <-release:
			return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: time.Now()}, nil
		case <-ctx.Done():
			return evidenceCohort{}, ctx.Err()
		}
	}
	c := cohortTestCache(t, d)
	google, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "google"})
	microsoft, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft"})
	c.lookup(google)
	if <-started != "google" {
		t.Fatal("first load changed")
	}
	c.lookup(microsoft)
	if active.Load() != 1 {
		t.Fatal("second brand bypassed the background load gate")
	}
	release <- struct{}{}
	if <-started != "microsoft" {
		t.Fatal("queued brand was not loaded")
	}
	release <- struct{}{}
	c.inflight.Wait()
	if maximum.Load() != 1 {
		t.Fatal("full-cohort reads overlapped")
	}
}

func TestEvidenceCohortCacheStopCancelsQueuedLoadWithoutStartingIt(t *testing.T) {
	d := &Database{}
	d.evidenceCohorts.configure(cohortTestConfig())
	started := make(chan struct{})
	var calls atomic.Int32
	d.evidenceCohorts.load = func(ctx context.Context, _ EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return evidenceCohort{}, ctx.Err()
	}
	c := cohortTestCache(t, d)
	google, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "google"})
	microsoft, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft"})
	c.lookup(google)
	<-started
	c.lookup(microsoft)
	c.stop()
	if calls.Load() != 1 {
		t.Fatal("queued cohort started a database read during shutdown")
	}
}

func TestEvidenceCohortCacheBoundsKeysByRecentDemand(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	blocking := int32(1)
	d := &Database{}
	cfg := cohortTestConfig()
	cfg.maxKeys = 2
	d.evidenceCohorts.configure(cfg)
	d.evidenceCohorts.load = func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&blocking) == 1 {
			started <- struct{}{}
			<-release
		}
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: time.Now()}, nil
	}
	c := cohortTestCache(t, d)
	filter := func(brand string) EvidenceFilter {
		f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: brand})
		return f
	}
	// Two refreshing cohorts fill the bound; a third brand is neither admitted
	// nor refreshed while nothing idle can be evicted.
	c.lookup(filter("alpha"))
	c.lookup(filter("beta"))
	<-started
	if _, ok := c.lookup(filter("gamma")); ok || atomic.LoadInt32(&calls) != 1 || len(c.entries) != 2 {
		t.Fatal("a third key evicted a refreshing cohort or scheduled a refresh")
	}
	release <- struct{}{}
	<-started
	close(release)
	c.inflight.Wait()
	atomic.StoreInt32(&blocking, 0)
	// alpha is the least recently requested idle cohort: gamma replaces it.
	c.lookup(filter("gamma"))
	c.inflight.Wait()
	if _, held := c.entries[evidenceCohortKey(filter("alpha"))]; held || len(c.entries) != 2 || atomic.LoadInt32(&calls) != 3 {
		t.Fatal("least recently requested cohort was not evicted")
	}
	if _, ok := c.lookup(filter("beta")); !ok {
		t.Fatal("recently requested cohort was evicted")
	}
	// A request for alpha is demand again: it reloads and evicts gamma, the
	// least recent of the remaining two.
	c.lookup(filter("alpha"))
	c.inflight.Wait()
	if _, held := c.entries[evidenceCohortKey(filter("gamma"))]; held || atomic.LoadInt32(&calls) != 4 {
		t.Fatal("eviction did not follow request recency")
	}
}

func TestEvidenceCohortCacheStopCancelsRefreshesAndSchedulesNoMore(t *testing.T) {
	var calls int32
	started := make(chan struct{}, 1)
	d := &Database{}
	d.evidenceCohorts.configure(cohortTestConfig())
	d.evidenceCohorts.load = func(ctx context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		atomic.AddInt32(&calls, 1)
		started <- struct{}{}
		<-ctx.Done()
		return evidenceCohort{}, ctx.Err()
	}
	c := cohortTestCache(t, d)
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude"})
	c.lookup(f)
	<-started
	done := make(chan struct{})
	go func() {
		d.StopEvidenceBrandCohorts()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not cancel the in-flight refresh")
	}
	if _, ok := c.lookup(f); ok {
		t.Fatal("a cancelled refresh published a cohort")
	}
	c.inflight.Wait()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatal("a refresh was scheduled after Stop")
	}
	d.StopEvidenceBrandCohorts() // idempotent
}
