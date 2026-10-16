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

var projectedTestColumns = strings.Split("seq,public_id,lat,long,class,severity,language,brand,created,kind,excerpt,impact,relationship,version,media,clean,platform,url,published,observed,ts,missing,actor,fetcher,private,key,external,channel,generation,account,null_time", ",")

func projectedTestRow(seq int) []driver.Value {
	return []driver.Value{seq, fmt.Sprint("r_test_", seq), 0, 0, "digital", 0.7, "fr", "claude", "2026-10-15 00:00:00.000000", "firsthand", "1", "1", "affected", 1, false,
		true, "custom", fmt.Sprintf("https://example.test/report/%d", seq), "2026-10-14 12:00:00.123456", nil, nil, false, "", "test", false, fmt.Sprintf("url:example.test/report/%d", seq), true, "", "", "", true}
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
	for _, forbidden := range []string{"external_ingest_index", "wire_submissions_raw", "JSON_TABLE", "JSON_EXTRACT", "canonical_url", "LIMIT", "WHERE report_kind"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("clean cache path retained source discovery/normalization or early filtering: %s", forbidden)
		}
	}
	for _, required := range []string{"COLLATE utf8mb4_bin", "WHEN m.seq IS NOT NULL", "rs.status IS NOT NULL AND rs.status<>'active'", "ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE", PublicVisibilityWhereSQL, "LEFT JOIN report_evidence ev", "MAX_EXECUTION_TIME(8000)"} {
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

func TestProjectedEvidenceCleanScopeGroupsAndHydratesExactNullableSelection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, winner, unrelated := projectedTestRow(42), projectedTestRow(43), projectedTestRow(44)
	first[5], first[6], first[8], first[18] = nil, nil, nil, "2026-10-14 11:00:00.123456"
	unrelated[9], unrelated[13], unrelated[20], unrelated[30] = "unknown", 0, "2026-10-15 00:00:00.000000", false
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnRows(sqlmock.NewRows(projectedTestColumns).AddRow(first...).AddRow(winner...).AddRow(unrelated...))
	// Any identity/header/normalizer read is unexpected on the all-clean path.
	mock.ExpectQuery("SELECT cr.seq,MIN").WithArgs("[42,43,44]").WillReturnRows(sqlmock.NewRows([]string{"seq", "case"}).AddRow(42, "family-1").AddRow(43, "family-1"))
	mock.ExpectQuery("WITH display AS").WithArgs("fr", projectedSelectedAnalysisArgument{}).WillReturnRows(sqlmock.NewRows([]string{"seq", "title", "summary", "brand"}).AddRow(43, "Chosen title", "Chosen summary", "Claude").AddRow(42, "Nullable sample", "Sample summary", "Claude"))
	mock.ExpectQuery("SELECT ev.seq,COALESCE").WithArgs("[43]").WillReturnRows(sqlmock.NewRows([]string{"seq", "excerpt", "impact", "reason"}).AddRow(43, "Original observation", "Blocked", "Direct"))
	mock.ExpectCommit()
	page, err := (&Database{db: db}).getEvidenceBrandPageProjected(context.Background(), EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 1, asOf: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if page.Overview.TotalReports != 3 || page.Overview.ReportsLast7Days != 1 || page.Total != 1 || page.KindCounts.All != 2 || page.KindCounts.Firsthand != 1 || page.KindCounts.Unknown != 1 || len(page.Reports) != 1 {
		t.Fatalf("full-scope exact grouping changed: %+v", page)
	}
	r := page.Reports[0]
	if r.Report.Seq != 43 || !r.Report.Timestamp.Equal(time.Unix(0, 0)) || r.Evidence.IngestedAt != nil || r.Analysis[0].Title != "Chosen title" || r.Evidence.SourcePublishedAt == nil || r.Evidence.SourcePublishedAt.Nanosecond() != 123456000 || len(r.Recurrence.SupportingSources) != 2 {
		t.Fatal("nullable selection, date precision, or hydration changed")
	}
	for _, sample := range r.Recurrence.SupportingSources {
		if sample.SourcePublishedAt == nil || sample.SourcePublishedAt.Nanosecond() != 0 {
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
	b[15], b[24] = false, true
	c[18] = "not-a-date"
	d[20], d[30] = "0000-00-00 00:00:00.0", false
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

func TestProjectedEvidencePartialOrDuplicateBaseNeverReturnsCounts(t *testing.T) {
	for _, mode := range []string{"stream_error", "duplicate", "missing_fresh_parent"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			row := projectedTestRow(42)
			if mode == "missing_fresh_parent" {
				row[15] = false
			}
			rows := sqlmock.NewRows(projectedTestColumns).AddRow(row...)
			if mode == "stream_error" {
				rows.AddRow(projectedTestRow(43)...).RowError(1, errors.New("partial"))
			} else if mode == "duplicate" {
				rows.AddRow(row...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows)
			if mode == "missing_fresh_parent" {
				mock.ExpectQuery("SELECT ids.seq,").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"seq", "id", "ts", "raw_type", "raw_id", "fetcher"}))
			}
			mock.ExpectRollback()
			page, err := (&Database{db: db}).getEvidenceBrandPageProjected(context.Background(), EvidenceFilter{BrandName: "claude", Kind: "all"})
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
