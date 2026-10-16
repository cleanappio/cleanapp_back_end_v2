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
	if strings.Count(query, "SET_VAR(tmp_table_size=67108864)") != 1 {
		t.Fatal("temporary-table ceiling must be local to the outer statement")
	}
	if !strings.Contains(query, "FROM representatives n CROSS JOIN overview_clock c") || strings.Contains(query, "FROM normalized n CROSS JOIN overview_clock c") {
		t.Fatal("raw overview must reuse every pre-deduplication representative row")
	}
	preferred := strings.Split(query, "), metadata AS (")[0]
	for _, field := range []string{"ra.title", "ra.summary", "ra.brand_display_name", "COALESCE(ev.classification_reason"} {
		if strings.Contains(preferred, field) {
			t.Fatalf("all-cohort window retains display body %s", field)
		}
	}
	for _, required := range []string{"FORCE INDEX (idx_ra_brand_valid_class_core)", "external_ingest_header_projection", "wire_evidence_projection", "eii_fallback.source=CASE WHEN eii_header.seq IS NULL", "wire_fallback.submission_id=CASE WHEN wire_header.submission_id IS NULL", "PARTITION BY source_key ORDER BY evidence_version DESC", "MIN(cr.case_id)", "AND f.family_rank <= 8 AND f.privacy_restricted = 0", "FROM tagged WHERE group_key IS NOT NULL GROUP BY group_key", "analysis_created_at", "'core',JSON_OBJECT(", "END) AS submitted_at"} {
		if !strings.Contains(query, required) {
			t.Fatalf("missing narrow-reader invariant %s", required)
		}
	}
	if strings.Index(query, "family_samples AS (") < strings.Index(query, "paged AS (") || !strings.Contains(query, "FROM paged p STRAIGHT_JOIN filtered f ON f.family_representative_seq=p.seq") {
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
		for _, required := range []string{"FROM paged p STRAIGHT_JOIN normalized n ON n.seq=p.seq", "FROM paged p STRAIGHT_JOIN filtered f ON f.family_representative_seq=p.seq AND f.family_rank <= 8 AND f.privacy_restricted = 0 STRAIGHT_JOIN normalized n ON n.seq=f.seq", "LEFT JOIN paged_details p ON TRUE", "'rank',f.family_rank", "'created',DATE_FORMAT(n.analysis_created_at"} {
			if !strings.Contains(query, required) {
				t.Fatalf("%s loses bounded detail/selection contract %s", sortBy, required)
			}
		}
		if strings.Contains(query, "family_representative_seq IN (SELECT seq FROM paged)") {
			t.Fatal("sample detail reads must start at the bounded page, not a late semijoin")
		}
	}
}

func TestNarrowEvidenceChoosesSourceIdentitiesBeforeWideMetadata(t *testing.T) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	query, _, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	start, end := strings.Index(query, "source_winners AS ("), strings.Index(query, "), candidates AS (")
	if start < 0 || end <= start {
		t.Fatal("source identity selection phase missing")
	}
	identity := query[start : end+1]
	for _, forbidden := range []string{"report_json", "provenance_json", "extensions_json", "source_url", "source_timestamp", "privacy_restricted", "canonical_url", "ra.*", "ra.title", "BINARY", "COLLATE", "ROW_NUMBER()"} {
		if strings.Contains(identity, forbidden) {
			t.Fatalf("source winner window carries wide data or changes native ordering: %s", forbidden)
		}
	}
	for _, required := range []string{
		"SELECT r.*,eii_selected.source,eii_selected.external_id,wire_selected.wire_submission_id",
		"FROM metadata r",
		"LEFT JOIN LATERAL (SELECT eii_choice.source,eii_choice.external_id",
		"external_ingest_index eii_choice FORCE INDEX(idx_seq) WHERE eii_choice.seq=r.seq",
		"ORDER BY eii_choice.source ASC,eii_choice.external_id ASC LIMIT 1) eii_selected ON TRUE",
		"LEFT JOIN LATERAL (SELECT wire_choice.submission_id AS wire_submission_id",
		"WHERE wire_choice.report_seq=r.seq",
		"wire_choice_header.report_seq=wire_choice.report_seq",
		"wire_choice_fallback.submission_id=CASE WHEN wire_choice_header.submission_id IS NULL THEN wire_choice.submission_id ELSE NULL END",
		"ORDER BY CASE WHEN wire_choice_header.submission_id IS NOT NULL THEN wire_choice_header.submitted_at ELSE wire_choice_fallback.submitted_at END DESC LIMIT 1) wire_selected ON TRUE",
	} {
		if !strings.Contains(identity, required) {
			t.Fatalf("source identity selection lost contract: %s", required)
		}
	}
	candidate := query[end:strings.Index(query, "), provenance AS (")]
	for _, required := range []string{"FROM source_winners r", "NO_MERGE(r)", "eii_header.seq=r.seq", "wire_header.submission_id=r.wire_submission_id AND wire_header.report_seq=r.seq", "r.public_id <> ''", PublicVisibilityWhereSQL, "FROM report_status rs", "FROM reports_owners ro", "1 AS source_rank", "r.created_at AS analysis_created_at"} {
		if !strings.Contains(candidate, required) {
			t.Fatalf("chosen source hydration lost metadata/privacy contract: %s", required)
		}
	}
	if strings.Contains(candidate, "ROW_NUMBER()") || strings.Contains(candidate, "LEFT JOIN external_ingest_index eii_choice") {
		t.Fatal("wide source metadata must not participate in source winner selection")
	}
	if strings.Count(query, "FROM preferred ra") != 1 || strings.Contains(candidate, "ra.") || strings.Contains(query, "JOIN preferred ra") {
		t.Fatal("chosen source hydration must reuse the one selected analysis/metadata scope")
	}
	if strings.Count(identity, "LIMIT 1") != 2 {
		t.Fatal("only the two per-report identity lookups may limit the source scope")
	}
	if !strings.Contains(query, "SELECT ra.seq,ra.language,ra.created_at,ra.brand_name,ra.classification,ra.severity_level,") {
		t.Fatal("metadata must carry the exact nullable selected analysis tuple")
	}
}

func TestNarrowEvidencePreferredAnalysisWindowExcludesPerReportEvidence(t *testing.T) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	query, _, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	preferred := strings.Split(query, "), metadata AS (")[0]
	for _, forbidden := range []string{"report_evidence", "ev.", "'' AS", "brand_display_name", "ra.title", "ra.summary", "report_kind", "entity_relationship"} {
		if strings.Contains(preferred, forbidden) {
			t.Fatalf("preferred analysis ranking carries report/display data: %s", forbidden)
		}
	}
	for _, required := range []string{"ra.seq,ra.language,ra.created_at,ra.brand_name,ra.classification,CAST(ra.severity_level AS DOUBLE) AS severity_level", "ROW_NUMBER() OVER (PARTITION BY ra.seq ORDER BY (ra.language = ?) DESC, (ra.language = 'en') DESC, ra.language ASC, ra.created_at DESC) AS analysis_rank"} {
		if !strings.Contains(preferred, required) {
			t.Fatalf("preferred analysis ranking/tuple changed: %s", required)
		}
	}
	candidate := query[strings.Index(query, "), candidates AS ("):strings.Index(query, "), provenance AS (")]
	for _, required := range []string{"LEFT JOIN report_evidence ev ON ev.seq=r.seq", "COALESCE(ev.report_kind,'unknown')", "COALESCE(ev.entity_relationship,'unknown') AS entity_relationship", "COALESCE(ev.evidence_version,0) AS evidence_version", "COALESCE(ev.evidence_excerpt<>'',FALSE)", "COALESCE(ev.reported_impact<>'',FALSE)", "'' AS title", "'' AS summary", "'' AS brand_display_name", "'' AS classification_reason"} {
		if !strings.Contains(candidate, required) {
			t.Fatalf("post-selection evidence defaults/flags missing: %s", required)
		}
	}
	if strings.Count(query, "LEFT JOIN report_evidence ev") != 1 {
		t.Fatal("per-report evidence must join once after source identity selection")
	}
}

func TestNarrowEvidenceStoresSourceRankInputsAndCountsWholePartition(t *testing.T) {
	f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "microsoft", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	query, _, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "rank_input AS (SELECT "+narrowEvidenceRankColumns+" FROM normalized)") || !strings.Contains(query, "SELECT /*+ NO_MERGE(rank_input) */ rank_input.*") || strings.Count(query, "NO_MERGE(rank_input)") != 1 {
		t.Fatal("source windows must consume stored compact rank inputs")
	}
	if strings.Count(query, "OVER (PARTITION BY source_key ORDER BY "+narrowEvidenceSourceOrder) != 3 {
		t.Fatal("source ranking and import counts must share the exact native SQL ordering")
	}
	if !strings.Contains(query, "COUNT(*) OVER (PARTITION BY source_key ORDER BY "+narrowEvidenceSourceOrder+" ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) AS import_count FROM rank_input") {
		t.Fatal("ordered import count must include all rows, not a running or peer count")
	}
	if strings.Contains(query, "COUNT(*) OVER (PARTITION BY source_key) AS import_count") {
		t.Fatal("partition-only count prevents reuse of the source winner sort")
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
