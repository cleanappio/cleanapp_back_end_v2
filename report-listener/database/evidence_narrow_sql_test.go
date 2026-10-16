package database

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNarrowEvidenceRetainsOracleGroupingButDefersDisplayBodies(t *testing.T) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Kind: "firsthand", Sort: "recent", Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	query, _, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(query, "MAX_EXECUTION_TIME(8000)") != 1 {
		t.Fatal("narrow statement must retain the public eight-second server work bound")
	}
	preferred := strings.Split(query, "), metadata AS (")[0]
	for _, field := range []string{"ra.title", "ra.summary", "ra.brand_display_name", "COALESCE(ev.classification_reason"} {
		if strings.Contains(preferred, field) {
			t.Fatalf("all-cohort window retains display body %s", field)
		}
	}
	for _, required := range []string{"FORCE INDEX (idx_ra_brand_valid_class_core)", "external_ingest_header_projection", "wire_evidence_projection", "eii_fallback.source=CASE WHEN eii_header.seq IS NULL", "wire_fallback.submission_id=CASE WHEN wire_header.submission_id IS NULL", "PARTITION BY source_key ORDER BY evidence_version DESC", "MIN(cr.case_id)", "AND f.family_rank <= 8 AND f.privacy_restricted = 0", "COUNT(DISTINCT group_key)", "analysis_created_at", "'core',JSON_OBJECT(", "END) AS submitted_at"} {
		if !strings.Contains(query, required) {
			t.Fatalf("missing narrow-reader invariant %s", required)
		}
	}
	if strings.Index(query, "family_samples AS (") < strings.Index(query, "paged AS (") || !strings.Contains(query, "f.family_representative_seq IN (SELECT seq FROM paged)") {
		t.Fatal("sample JSON must be aggregated only after family pagination")
	}
	for _, forbidden := range []string{"ws.", "eii.", "LEFT JOIN wire_submissions_raw ws ", "LEFT JOIN external_ingest_index eii "} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("unprojected source reference %s", forbidden)
		}
	}
}

func TestNarrowEvidenceKeepsDisplayIdentityFieldsOutsideRankingWindows(t *testing.T) {
	for _, sortBy := range []string{"signal", "recent", "recurrence"} {
		f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Kind: "coverage", Sort: sortBy})
		if err != nil {
			t.Fatal(err)
		}
		query, _, err := narrowEvidenceBrandPageQuery(f)
		if err != nil {
			t.Fatal(err)
		}
		start, end := strings.Index(query, " representatives AS ("), strings.Index(query, " paged_details AS (")
		if start < 0 || end < start {
			t.Fatal("rank core or bounded detail phase missing")
		}
		rank := query[start:end]
		for _, forbidden := range []string{"normalized.*", "public_id", "source_url", "canonical_url", "original_id", "source_author", "analysis_created_at", "brand_name", "language", "latitude", "longitude", "title", "summary"} {
			if strings.Contains(rank, forbidden) {
				t.Fatalf("%s ranking windows carry late detail %s", sortBy, forbidden)
			}
		}
		for _, required := range []string{"FROM paged p JOIN normalized n ON n.seq=p.seq", "FROM filtered f JOIN normalized n ON n.seq=f.seq WHERE f.family_representative_seq IN (SELECT seq FROM paged)", "LEFT JOIN paged_details p ON TRUE", "'rank',f.family_rank", "'created',DATE_FORMAT(n.analysis_created_at"} {
			if !strings.Contains(query, required) {
				t.Fatalf("%s loses bounded detail/selection contract %s", sortBy, required)
			}
		}
	}
}

type narrowSelectedCoreArgument struct{}

func (narrowSelectedCoreArgument) Match(value driver.Value) bool {
	var rows []struct {
		Seq      int      `json:"seq"`
		Language *string  `json:"language"`
		Created  *string  `json:"created"`
		Severity *float64 `json:"severity"`
	}
	text, ok := value.(string)
	if !ok || json.Unmarshal([]byte(text), &rows) != nil || len(rows) != 2 {
		return false
	}
	return rows[0].Seq == 42 && rows[0].Language == nil && rows[0].Created == nil && rows[0].Severity == nil && rows[1].Seq == 43 && rows[1].Language != nil && *rows[1].Language == "fr"
}

func TestNarrowEvidenceHydratesOnlyPageAndSampleExactAnalysisSelections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "firsthand", Language: "fr", Limit: 1})
	query, args, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg
	}
	columns := strings.Split("all,firsthand,coverage,unknown,total,high,medium,total_reports,reports_last_30_days,reports_last_7_days,reports_previous_7_days,latest_report_at,seq,public_id,ts,latitude,longitude,title,summary,classification,severity_level,language,brand_name,brand_display_name,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version,source_platform,source_url,source_timestamp,observed_at,actor_kind,fetcher_id,privacy_restricted,source_key,import_count,case_id,related_observation_count,has_original_media,external_source,intake_channel,generation_method,source_account_key,group_key,supporting_report_count,source_account_count,duplicate_import_count,supporting_sources,latest_observed_at,latest_source_published_at,ingestion_time_missing,ingested_at_null,private_core", ",")
	added := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	core := `{"seq":42,"language":null,"created":null,"brand":"claude","classification":"digital","severity":null}`
	// Simulate a late detail join returning samples in physical rather than rank
	// order; nullable selected analysis metadata must still bind exactly.
	samples := `[{"rank":2,"core":{"seq":43,"language":"fr","created":"2026-10-15 00:00:00.000000","brand":"claude","classification":"digital","severity":0.7},"title":"","source_url":"https://example.test/43","report_kind":"firsthand","source_published_at":null},{"rank":1,"core":` + core + `,"title":"","source_url":"https://example.test/42","report_kind":"firsthand","source_published_at":null}]`
	rows := sqlmock.NewRows(columns).AddRow(50000, 9, 2, 49989, 9, 1, 1, 50001, 7, 4, 2, added, 42, "r_test", added, 0, 0, "", "", "digital", 0, "", "claude", "", "firsthand", "1", "1", "", "affected", 1, "github_issue", "https://example.test/42", nil, nil, "machine", "fetcher", false, "url:example.test/42", 1, "case-1", 2, false, true, "wire", "native", "github:user", "family:case-1", 2, 1, 0, samples, nil, nil, false, false, core)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(values...).WillReturnRows(rows)
	mock.ExpectQuery("WITH display AS").WithArgs("fr", narrowSelectedCoreArgument{}).WillReturnRows(sqlmock.NewRows([]string{"seq", "title", "summary", "brand"}).AddRow(42, "Actual title", "Actual summary", "Claude").AddRow(43, "Related title", "Related summary", "Claude"))
	mock.ExpectQuery("SELECT ev.seq,COALESCE").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"seq", "excerpt", "impact", "reason"}).AddRow(42, "Verbatim observation", "Blocked", "Direct report"))
	mock.ExpectCommit()
	page, err := (&Database{db: db}).getEvidenceBrandPageNarrowSQL(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 9 || page.KindCounts.All != 50000 || len(page.Reports) != 1 || page.Reports[0].Analysis[0].Title != "Actual title" || *page.Reports[0].Evidence.Excerpt != "Verbatim observation" {
		t.Fatalf("late hydration lost public fields/counts: %+v", page)
	}
	if len(page.Reports[0].Recurrence.SupportingSources) != 2 || page.Reports[0].Recurrence.SupportingSources[1].Title != "Related title" {
		t.Fatal("late hydration lost supporting source title")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowEvidenceFailureCannotReturnApparentlyExactPartialPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnError(errors.New("query unavailable"))
	mock.ExpectRollback()
	page, err := (&Database{db: db}).getEvidenceBrandPageNarrowSQL(context.Background(), EvidenceFilter{BrandName: "microsoft", Kind: "all"})
	if err == nil || page.Total != 0 || len(page.Reports) != 0 {
		t.Fatal("failed exact SQL query returned a usable page")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
