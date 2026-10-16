package database

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestHybridEvidenceReadsUnfilteredNormalizedScopeWithOnlyScopeArgs(t *testing.T) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand", Language: "fr", Sort: "recent", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	query, args, err := hybridEvidenceNormalizedQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []interface{}{f.Language}
	for _, brand := range f.Brands {
		wantArgs = append(wantArgs, brand)
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized scope retained non-scope parameters: %v", args)
	}
	for _, forbidden := range []string{"rank_input AS", "representatives AS", "family_samples AS", "family_facets AS", "scope_counts AS", "overview_clock AS", "NO_MERGE(r)", "LIMIT ?", "WHERE report_kind"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("fused stream retained aggregation/paging/materialization: %s", forbidden)
		}
	}
	for _, required := range []string{"NO_MERGE(candidates)", "NO_MERGE(provenance)", "NO_MERGE(normalized)", "MAX_EXECUTION_TIME(8000) SET_VAR(tmp_table_size=67108864)", "seq,public_id,ts,", "classification,severity_level,language,brand_name", "DATE_FORMAT(analysis_created_at", "LEFT JOIN LATERAL", "wire_header.report_seq=r.seq"} {
		if !strings.Contains(query, required) {
			t.Fatalf("fused stream lost normalized/null/source contract: %s", required)
		}
	}
	other := f
	other.Kind, other.Sort, other.Limit, other.Offset = "all", "recurrence", 50, 100
	otherQuery, otherArgs, err := hybridEvidenceNormalizedQuery(other)
	if err != nil || query != otherQuery || !reflect.DeepEqual(args, otherArgs) {
		t.Fatal("kind/sort/pagination changed the full normalized report universe")
	}
}

var hybridTestColumns = strings.Split("seq,public_id,ts,latitude,longitude,classification,severity,language,brand,kind,excerpt,impact,relationship,version,platform,url,published,observed,actor,fetcher,private,source_key,media,external,channel,generation,account,missing_time,created", ",")

func hybridTestRow(seq int) []driver.Value {
	added := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	id := strconv.Itoa(seq)
	return []driver.Value{seq, "r_test_" + id, added, 0, 0, "digital", 0.7, "fr", "claude", "firsthand", "1", "1", "affected", 1,
		"github", "https://example.test/report/" + id, added, nil, "machine", "test", false, "url:example.test/report/" + id, false, true, "wire", "native", nil, false, "2026-10-15 00:00:00.000000"}
}

type hybridSelectedAnalysisArgument struct{}

func (hybridSelectedAnalysisArgument) Match(value driver.Value) bool {
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

func TestHybridEvidenceGroupsFullScopeAndHydratesOnlyChosenNullableAnalyses(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 1, asOf: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)})
	query, args, err := hybridEvidenceNormalizedQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]driver.Value, len(args))
	for i := range args {
		values[i] = args[i]
	}
	first, winner, unrelated := hybridTestRow(42), hybridTestRow(43), hybridTestRow(44)
	first[2], first[6], first[7], first[28] = nil, nil, nil, nil
	winner[2] = nil
	published := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	first[16], winner[16] = published.Add(-time.Hour), published
	unrelated[9], unrelated[13] = "unknown", 0
	rows := sqlmock.NewRows(hybridTestColumns).AddRow(first...).AddRow(winner...).AddRow(unrelated...)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(values...).WillReturnRows(rows)
	mock.ExpectQuery("SELECT cr.seq,MIN").WithArgs("[42,43,44]").WillReturnRows(sqlmock.NewRows([]string{"seq", "case"}).AddRow(42, "family-1").AddRow(43, "family-1"))
	mock.ExpectQuery("WITH display AS").WithArgs("fr", hybridSelectedAnalysisArgument{}).WillReturnRows(sqlmock.NewRows([]string{"seq", "title", "summary", "brand"}).AddRow(43, "Chosen title", "Chosen summary", "Claude").AddRow(42, "Nullable sample", "Sample summary", "Claude"))
	mock.ExpectQuery("SELECT ev.seq,COALESCE").WithArgs("[43]").WillReturnRows(sqlmock.NewRows([]string{"seq", "excerpt", "impact", "reason"}).AddRow(43, "Original observation", "Blocked", "Direct"))
	mock.ExpectCommit()
	page, err := (&Database{db: db}).getEvidenceBrandPageHybrid(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if page.Overview.TotalReports != 3 || page.Overview.ReportsLast7Days != 1 || page.Total != 1 || page.KindCounts.All != 2 || page.KindCounts.Firsthand != 1 || page.KindCounts.Unknown != 1 || len(page.Reports) != 1 {
		t.Fatalf("full-scope overview/facets/grouping changed: %+v", page)
	}
	r := page.Reports[0]
	if r.Report.Seq != 43 || !r.Report.Timestamp.Equal(time.Unix(0, 0)) || r.Evidence.IngestedAt != nil || r.Analysis[0].Title != "Chosen title" || r.Evidence.Excerpt == nil || *r.Evidence.Excerpt != "Original observation" {
		t.Fatal("NULL ranking, public epoch fallback, or bounded display fields changed")
	}
	if r.Evidence.SourcePublishedAt == nil || r.Evidence.SourcePublishedAt.Nanosecond() != published.Nanosecond() || len(r.Recurrence.SupportingSources) != 2 || r.Recurrence.SupportingSources[1].Title != "Nullable sample" {
		t.Fatal("selected source precision or supporting-source rank changed")
	}
	for _, sample := range r.Recurrence.SupportingSources {
		if sample.SourcePublishedAt == nil || sample.SourcePublishedAt.Nanosecond() != 0 {
			t.Fatal("supporting source date must retain the SQL oracle's whole-second format")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHybridEvidenceRejectsPartialOrDuplicateNormalizedStreams(t *testing.T) {
	for _, mode := range []string{"stream_error", "duplicate_seq"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows := sqlmock.NewRows(hybridTestColumns).AddRow(hybridTestRow(42)...)
			if mode == "stream_error" {
				rows.AddRow(hybridTestRow(43)...).RowError(1, errors.New("incomplete stream"))
			} else {
				rows.AddRow(hybridTestRow(42)...)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH preferred").WillReturnRows(rows)
			mock.ExpectRollback()
			page, err := (&Database{db: db}).getEvidenceBrandPageHybrid(context.Background(), EvidenceFilter{BrandName: "claude", Kind: "all"})
			if err == nil || page.Total != 0 || page.KindCounts.All != 0 || len(page.Reports) != 0 {
				t.Fatal("incomplete/duplicate normalized data produced apparently exact public counts")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
