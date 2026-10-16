package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/evidencesourceprojection"
	"github.com/DATA-DOG/go-sqlmock"
)

var projectedTestColumns = strings.Split("seq,class,severity,language,brand,created,kind,excerpt,impact,relationship,version,media,clean,published,observed,ts,missing,private,key,account,null_time,requested,english,language_weight,analysis_created", ",")

func projectedTestRow(seq int) []driver.Value {
	return []driver.Value{seq, "digital", 0.7, "fr", "claude", "2026-10-15 00:00:00.000000", "firsthand", "1", "1", "affected", 1, false,
		true, "2026-10-14 12:00:00.123456", nil, nil, false, false, fmt.Sprintf("url:example.test/report/%d", seq), "", true, 1, 0, []byte("fr"), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
}

var projectedOutputTestColumns = strings.Split("seq,public_id,lat,long,clean,platform,url,actor,fetcher,external,channel,generation", ",")

func projectedOutputTestRow(seq int) []driver.Value {
	return []driver.Value{seq, fmt.Sprint("r_test_", seq), 1.25, 2.5, true, "custom", fmt.Sprintf("https://example.test/report/%d", seq), "human", "test", true, "web", "human_authored_public"}
}

type projectedSelectedAnalysisArgument struct{}

func (projectedSelectedAnalysisArgument) Match(value driver.Value) bool {
	var selected []struct {
		Seq            int      `json:"seq"`
		Language       *string  `json:"language"`
		Created        *string  `json:"created"`
		Brand          string   `json:"Brand"`
		Classification string   `json:"Classification"`
		Severity       *float64 `json:"severity"`
	}
	text, ok := value.(string)
	if !ok || json.Unmarshal([]byte(text), &selected) != nil || len(selected) != 2 {
		return false
	}
	seen := map[int]bool{}
	for _, row := range selected {
		if row.Brand != "claude" || row.Classification != "digital" {
			return false
		}
		switch row.Seq {
		case 42:
			if row.Language != nil || row.Created != nil || row.Severity != nil {
				return false
			}
		case 43:
			if row.Language == nil || *row.Language != "fr" || row.Created == nil || *row.Created != "2026-10-15 00:00:00.000000" || row.Severity == nil || *row.Severity != 0.7 {
				return false
			}
		default:
			return false
		}
		seen[row.Seq] = true
	}
	return seen[42] && seen[43]
}

func TestProjectedEvidenceReadsCompleteLiveScopeWithoutSourceJoins(t *testing.T) {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand", Language: "fr", Sort: "recent", Limit: 1})
	query, args := projectedEvidenceBaseQuery(f)
	want := []interface{}{f.Language}
	for _, brand := range f.Brands {
		want = append(want, brand)
	}
	if !reflect.DeepEqual(args, want) || !strings.Contains(query, evidencesourceprojection.CleanPredicate("p")) {
		t.Fatal("projected base lost exact scope arguments or clean-generation contract")
	}
	for _, forbidden := range []string{"external_ingest_index", "wire_submissions_raw", "JSON_TABLE", "JSON_EXTRACT", "canonical_url", "LIMIT", "WHERE report_kind", "ROW_NUMBER", "analysis_rank"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("clean cache path retained source discovery/normalization or early filtering: %s", forbidden)
		}
	}
	projection := query[strings.Index(query, ") SELECT /*+"):strings.Index(query, " FROM preferred ra")]
	for _, deferred := range []string{" AS public_id", "m.latitude", "m.longitude", "p.source_platform", "p.source_url", "p.actor_kind", "p.fetcher_id", "p.external_source", "p.intake_channel", "p.generation_method"} {
		// The cleanliness predicate still validates the whole source tuple. Only
		// its selected output fields are deferred, never its completeness guard.
		projection = strings.ReplaceAll(projection, evidencesourceprojection.CleanPredicate("p"), "clean_generation")
		if strings.Contains(projection, deferred) {
			t.Fatalf("output-only field remains in the full stream: %s", deferred)
		}
	}
	for _, required := range []string{"COLLATE utf8mb4_bin", "WHEN m.seq IS NOT NULL", "rs.status IS NOT NULL AND rs.status<>'active'", "ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE", PublicVisibilityWhereSQL, "LEFT JOIN report_evidence ev", "MAX_EXECUTION_TIME(8000)", "WEIGHT_STRING(ra.language AS CHAR(2))", "ra.analysis_requested,ra.analysis_english,ra.analysis_language_weight,ra.created_at"} {
		if !strings.Contains(query, required) {
			t.Fatalf("live publication/evidence contract missing: %s", required)
		}
	}
	f.Kind, f.Sort, f.Limit, f.Offset = "all", "recurrence", 50, 100
	other, otherArgs := projectedEvidenceBaseQuery(f)
	if query != other || !reflect.DeepEqual(args, otherArgs) {
		t.Fatal("kind/order/page changed full live report universe")
	}
}

func TestProjectedEvidencePublicationGuardsKeepCorrelatedOwnerCountAndExactScope(t *testing.T) {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Language: "en"})
	query, _ := projectedEvidenceBaseQuery(f)
	hinted := "(SELECT /*+ JOIN_INDEX(ro idx_is_public) */ COUNT(*) FROM reports_owners ro WHERE ro.seq=ra.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)=0"
	status := "NOT EXISTS(SELECT 1 FROM report_status rs WHERE rs.seq=ra.seq AND rs.status IS NOT NULL AND rs.status<>'active')"
	if strings.Count(query, hinted) != 1 || strings.Count(query, "JOIN_INDEX(") != 1 || strings.Count(query, status) != 1 || strings.Count(query, "NOT EXISTS(") != 1 || strings.Count(query, "COUNT(*)") != 1 || strings.Contains(query, "ROW_NUMBER() OVER") || strings.Contains(query, "analysis_rank") || !strings.Contains(query, "WEIGHT_STRING(ra.language AS CHAR(2))") || !strings.Contains(query, "ra.analysis_requested,ra.analysis_english,ra.analysis_language_weight,ra.created_at") || !strings.Contains(query, "WHERE ra.seq IS NOT NULL") {
		t.Fatal("correlated owner count or status existence guard changed exact blockers or preferred analysis")
	}
	if strings.Contains(query, "FORCE INDEX (idx_is_public)") || strings.Contains(query, "NOT IN") || strings.Contains(query, "LIMIT") || !strings.Contains(query, "MAX_EXECUTION_TIME(8000)") {
		t.Fatal("publication guards introduced a required index, NULL-unsafe membership, or cohort/deadline change")
	}
}

func TestProjectedEvidenceOutputHydratesPageAndSamplesWithoutReplacingFreshSources(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	grouped := evidenceGroupedPage{
		Records:           []evidenceRecord{{Seq: 42}, {Seq: 43, SourceURL: "https://fresh.test/page", SourcePlatform: "fresh", ActorKind: "machine", FetcherID: "fresh-fetcher", ExternalSource: true, IntakeChannel: "archive", GenerationMethod: "shared"}},
		SupportingSamples: map[int][]evidenceRecord{42: {{Seq: 44, SourceURL: "https://fresh.test/sample", SourcePlatform: "fresh-sample"}}, 43: {{Seq: 42}}},
	}
	page, sample := projectedOutputTestRow(43), projectedOutputTestRow(44)
	page[4], sample[4] = false, false // dirty source values are deliberately stale
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT ids.seq,").WithArgs("[42,44,43]").WillReturnRows(sqlmock.NewRows(projectedOutputTestColumns).AddRow(projectedOutputTestRow(42)...).AddRow(sample...).AddRow(page...))
	mock.ExpectRollback()
	tx, _ := db.Begin()
	if err := hydrateProjectedEvidenceOutput(context.Background(), tx, &grouped, map[int]bool{43: true, 44: true}); err != nil {
		t.Fatal(err)
	}
	if grouped.Records[0].PublicID != "r_test_42" || grouped.Records[0].SourceURL != "https://example.test/report/42" || grouped.SupportingSamples[43][0].SourceURL != grouped.Records[0].SourceURL {
		t.Fatal("clean page/sample output was not hydrated coherently")
	}
	if r := grouped.Records[1]; r.PublicID != "r_test_43" || r.SourceURL != "https://fresh.test/page" || r.SourcePlatform != "fresh" || r.ActorKind != "machine" || r.FetcherID != "fresh-fetcher" || !r.ExternalSource || r.IntakeChannel != "archive" || r.GenerationMethod != "shared" {
		t.Fatal("late metadata replaced the exact fresh source tuple")
	}
	if r := grouped.SupportingSamples[42][0]; r.PublicID != "r_test_44" || r.SourceURL != "https://fresh.test/sample" || r.SourcePlatform != "fresh-sample" {
		t.Fatal("late sample metadata replaced exact fresh source values")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectedEvidenceOutputFailsClosedOnIncompleteDuplicateOrDirtyGeneration(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "dirty", "unknown", "null_public_id", "stream_error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			row := projectedOutputTestRow(42)
			if mode == "dirty" {
				row[4] = false
			} else if mode == "unknown" {
				row[0] = 99
			} else if mode == "null_public_id" {
				row[1] = nil
			}
			rows := sqlmock.NewRows(projectedOutputTestColumns)
			if mode != "missing" {
				rows.AddRow(row...)
			}
			if mode == "duplicate" {
				rows.AddRow(row...)
			} else if mode == "stream_error" {
				rows.AddRow(projectedOutputTestRow(43)...).RowError(1, errors.New("partial"))
			}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT ids.seq,").WithArgs("[42]").WillReturnRows(rows)
			mock.ExpectRollback()
			tx, _ := db.Begin()
			grouped := evidenceGroupedPage{Records: []evidenceRecord{{Seq: 42}}}
			if err := hydrateProjectedEvidenceOutput(context.Background(), tx, &grouped, nil); err == nil || grouped.Records[0].PublicID != "" {
				t.Fatal("partial or stale output was accepted or partially applied")
			}
			_ = tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicEvidenceProjectedCleanScopeGroupsAndHydratesExactNullableSelection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, winner, unrelated := projectedTestRow(42), projectedTestRow(43), projectedTestRow(44)
	first[2], first[3], first[5], first[13] = nil, nil, nil, "2026-10-14 11:00:00.123456"
	first[21], first[22], first[23], first[24] = nil, nil, nil, nil
	unrelated[6], unrelated[10], unrelated[15], unrelated[20] = "unknown", 0, "2026-10-15 00:00:00.000000", false
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnRows(sqlmock.NewRows(projectedTestColumns).AddRow(first...).AddRow(winner...).AddRow(unrelated...))
	// Any identity/header/normalizer read is unexpected on the all-clean path.
	mock.ExpectQuery("SELECT cr.seq,MIN").WithArgs("[42,43,44]").WillReturnRows(sqlmock.NewRows([]string{"seq", "case"}).AddRow(42, "family-1").AddRow(43, "family-1"))
	mock.ExpectQuery("SELECT ids.seq,").WithArgs("[43,42]").WillReturnRows(sqlmock.NewRows(projectedOutputTestColumns).AddRow(projectedOutputTestRow(43)...).AddRow(projectedOutputTestRow(42)...))
	mock.ExpectQuery("WITH display AS").WithArgs("fr", projectedSelectedAnalysisArgument{}).WillReturnRows(sqlmock.NewRows([]string{"seq", "title", "summary", "brand"}).AddRow(43, "Chosen title", "Chosen summary", "Claude").AddRow(42, "Nullable sample", "Sample summary", "Claude"))
	mock.ExpectQuery("SELECT ev.seq,COALESCE").WithArgs("[43]").WillReturnRows(sqlmock.NewRows([]string{"seq", "excerpt", "impact", "reason"}).AddRow(43, "Original observation", "Blocked", "Direct"))
	mock.ExpectCommit()
	page, err := (&Database{db: db}).GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 1, asOf: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if page.Overview.TotalReports != 3 || page.Overview.ReportsLast7Days != 1 || page.Total != 1 || page.KindCounts.All != 2 || page.KindCounts.Firsthand != 1 || page.KindCounts.Unknown != 1 || len(page.Reports) != 1 {
		t.Fatalf("full-scope exact grouping changed: %+v", page)
	}
	r := page.Reports[0]
	if r.Report.Seq != 43 || r.Report.PublicID != "r_test_43" || r.Report.Latitude != 1.25 || r.Report.Longitude != 2.5 || !r.Report.Timestamp.Equal(time.Unix(0, 0)) || r.Evidence.IngestedAt != nil || r.Analysis[0].Title != "Chosen title" || r.Evidence.SourcePublishedAt == nil || r.Evidence.SourcePublishedAt.Nanosecond() != 123456000 || len(r.Recurrence.SupportingSources) != 2 {
		t.Fatal("nullable selection, date precision, or hydration changed")
	}
	for _, sample := range r.Recurrence.SupportingSources {
		if sample.SourceURL == nil || !strings.HasPrefix(*sample.SourceURL, "https://example.test/report/") || sample.SourcePublishedAt == nil || sample.SourcePublishedAt.Nanosecond() != 0 {
			t.Fatal("sample whole-second oracle format changed")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectedEvidenceDirtyAndMalformedEntriesRemainExactMisses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, b, c, d := projectedTestRow(42), projectedTestRow(43), projectedTestRow(44), projectedTestRow(45)
	b[12], b[17] = false, true
	c[13] = "not-a-date"
	d[15], d[20] = "0000-00-00 00:00:00.0", false
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnRows(sqlmock.NewRows(projectedTestColumns).AddRow(a...).AddRow(b...).AddRow(c...).AddRow(d...))
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
	all, _, _, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || !reflect.DeepEqual(missing, []int{43, 44}) || !all[3].IngestedAt.IsZero() || all[3].IngestedAtNull {
		t.Fatal("dirty/malformed cache row was omitted, trusted, or zero/NULL state changed")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicEvidenceProjectedPartialOrDuplicateBaseNeverReturnsCounts(t *testing.T) {
	for _, mode := range []string{"stream_error", "duplicate", "missing_fresh_parent"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			row := projectedTestRow(42)
			if mode == "missing_fresh_parent" {
				row[12] = false
			}
			rows := sqlmock.NewRows(projectedTestColumns).AddRow(row...)
			if mode == "stream_error" {
				rows.AddRow(projectedTestRow(43)...).RowError(1, errors.New("partial"))
			} else if mode == "duplicate" {
				inconsistent := projectedTestRow(42)
				inconsistent[17] = true
				rows.AddRow(inconsistent...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows)
			if mode == "missing_fresh_parent" {
				mock.ExpectQuery("SELECT ids.seq,").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"seq", "id", "ts", "raw_type", "raw_id", "fetcher"}))
			}
			mock.ExpectRollback()
			page, err := (&Database{db: db}).GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "claude", Kind: "all"})
			if err == nil || page.Total != 0 || page.KindCounts.All != 0 || len(page.Reports) != 0 {
				t.Fatal("partial base or fallback produced exact-looking counts")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFreshEvidenceSourcesIncludeHiddenParentsWithoutAnalysis(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := fmt.Sprintf("%x", sha1.Sum([]byte("reddit:t3_abc")))[:32]
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT ids.seq,").WithArgs("[42,43]").WillReturnRows(sqlmock.NewRows([]string{"seq", "id", "ts", "type", "raw_id", "fetcher"}).AddRow(42, "reddit_t3_abc", "2026-03-01 00:00:00.000000", nil, nil, "archive"))
	mock.ExpectQuery("SELECT e.seq,e.source,e.external_id").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(42, "reddit_archive", id))
	mock.ExpectQuery("SELECT ids.seq,ids.source,ids.id").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id", "url", "published", "created"}).AddRow(42, "reddit_archive", id, nil, nil, "2026-10-15 00:00:00.000000"))
	mock.ExpectQuery("SELECT ws.submission_id,ws.report_seq").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"id", "seq"}))
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	got, err := loadFreshEvidenceSourceRecords(context.Background(), tx, []int{42, 43})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[42].SourceKey != "source:reddit_archive:archive:"+id || got[42].PublishedAt == nil || got[42].PublishedAt.Month() != time.March {
		t.Fatal("existing hidden/no-analysis source was lost or missing parent invented")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceProjectionDatesKeepNullZeroAndFractionPrecision(t *testing.T) {
	for _, value := range []string{"2026-10-15 12:30:45", "2026-10-15 12:30:45.1", "2026-10-15 12:30:45.123456", "0000-00-00 00:00:00", "0000-00-00 00:00:00.0", "0000-00-00 00:00:00.000000"} {
		got, ok := sourceProjectionDate(sql.NullString{String: value, Valid: true})
		if !ok || got == nil {
			t.Fatalf("valid SQL date rejected: %q", value)
		}
	}
	got, ok := sourceProjectionDate(sql.NullString{})
	if !ok || got != nil {
		t.Fatal("SQL NULL date changed")
	}
	for _, literal := range []string{"2026-99-15 12:30:45", "2026-10-15 12:30:45,1", "2026-10-15 12:30:45.1234567", "2026-10-15T12:30:45"} {
		if _, ok := sourceProjectionDate(sql.NullString{String: literal, Valid: true}); ok {
			t.Fatal("malformed date accepted")
		}
	}
}

func TestProjectedEvidenceSelectsAnalysisFromNativeKeysInEitherStreamOrder(t *testing.T) {
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	newer := older.AddDate(0, 1, 0)
	analysis := func(language driver.Value, requested, english driver.Value, weight []byte, created *time.Time, severity driver.Value) []driver.Value {
		row := projectedTestRow(42)
		row[2], row[3], row[21], row[22], row[23] = severity, language, requested, english, weight
		if created == nil {
			row[5], row[24] = nil, nil
		} else if created.IsZero() {
			row[5], row[24] = "0000-00-00 00:00:00.000000", *created
		} else {
			row[5], row[24] = created.Format("2006-01-02 15:04:05.000000"), *created
		}
		return row
	}
	zero := time.Time{}
	for _, test := range []struct {
		name   string
		loser  []driver.Value
		winner []driver.Value
	}{
		{"requested_before_newer_english", analysis("en", 0, 1, []byte("en"), &newer, 0.1), analysis("fr", 1, 0, []byte("fr"), &older, 0.8)},
		{"english_before_other_language", analysis("fr", 0, 0, []byte("fr"), &newer, 0.1), analysis("en", 0, 1, []byte("en"), &older, 0.8)},
		{"native_weight_before_utf8_bytes", analysis("z", 0, 0, []byte{0x20}, &newer, 0.1), analysis("é", 0, 0, []byte{0x10}, &older, 0.8)},
		{"newest_within_same_language", analysis("fr", 1, 0, []byte("fr"), &older, 0.1), analysis("fr", 1, 0, []byte("fr"), &newer, 0.8)},
		{"nonnull_language_before_sql_null", analysis(nil, nil, nil, nil, &newer, 0.1), analysis("de", 0, 0, []byte("de"), &older, 0.8)},
		{"nonnull_zero_date_before_sql_null", analysis("fr", 1, 0, []byte("fr"), nil, 0.9), analysis("fr", 1, 0, []byte("fr"), &zero, nil)},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse_%t", test.name, reverse), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				rows := sqlmock.NewRows(projectedTestColumns)
				if reverse {
					rows.AddRow(test.winner...).AddRow(test.loser...)
				} else {
					rows.AddRow(test.loser...).AddRow(test.winner...)
				}
				mock.ExpectBegin()
				mock.ExpectQuery("WITH preferred").WillReturnRows(rows)
				mock.ExpectRollback()
				tx, _ := db.Begin()
				f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Language: "fr"})
				all, selected, bySeq, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
				if err != nil || len(all) != 1 || len(selected) != 1 || bySeq[42] != 0 || len(missing) != 0 {
					t.Fatalf("analysis duplicates changed exact report universe: %v", err)
				}
				r, choice := all[0], selected[0]
				if r.Language != test.winner[3] || choice.languageNull || r.SeverityNull != (test.winner[2] == nil) || test.winner[2] != nil && r.Severity != test.winner[2].(float64) || choice.created.String != test.winner[5] {
					t.Fatal("native/nullable preferred tuple changed")
				}
				_ = tx.Rollback()
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestProjectedEvidenceDuplicateAnalysisKeepsOneDirtyReportAndCoherentTies(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprintf("dirty_%t", dirty), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows := sqlmock.NewRows(projectedTestColumns)
			for i := 0; i < 100; i++ {
				row := projectedTestRow(42)
				row[2], row[4], row[12] = float64(i)/100, fmt.Sprint("brand_", i), !dirty
				rows.AddRow(row...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows)
			mock.ExpectRollback()
			tx, _ := db.Begin()
			f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Language: "fr"})
			all, selected, bySeq, missing, err := loadProjectedEvidenceBase(context.Background(), tx, f)
			if err != nil || len(all) != 1 || len(selected) != 1 || len(bySeq) != 1 || len(missing) != map[bool]int{true: 1, false: 0}[dirty] {
				t.Fatalf("duplicate analyses multiplied report or dirty fallback scope: %v", err)
			}
			coherent := false
			for i := 0; i < 100; i++ {
				coherent = coherent || all[0].Brand == fmt.Sprint("brand_", i) && all[0].Severity == float64(i)/100
			}
			if !coherent {
				t.Fatal("unspecified analysis tie mixed fields from different analyses")
			}
			_ = tx.Rollback()
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
