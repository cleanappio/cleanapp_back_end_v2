package database

import (
	"context"
	"crypto/sha1"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
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
	query, args, err := hybridEvidenceInputQuery(f, "[]")
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []interface{}{f.Language}
	for _, brand := range f.Brands {
		wantArgs = append(wantArgs, brand)
	}
	wantArgs = append(wantArgs, "[]")
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized scope retained non-scope parameters: %v", args)
	}
	for _, forbidden := range []string{"rank_input AS", "representatives AS", "family_samples AS", "family_facets AS", "scope_counts AS", "overview_clock AS", "NO_MERGE(r)", "LATERAL", "eii_identities AS (", "wire_identities AS (", "LIMIT ?", "WHERE report_kind", "canonical_url", " AS source_key", "STR_TO_DATE", "SHA1("} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("fused stream retained aggregation/paging/materialization: %s", forbidden)
		}
	}
	for _, required := range []string{"MAX_EXECUTION_TIME(8000) SET_VAR(tmp_table_size=67108864)", "r.classification,r.severity_level,r.language,r.brand_name", "DATE_FORMAT(r.created_at", "DATE_FORMAT(r.ts", "COALESCE(r.id,'')", "chosen_source_ids AS (", "NO_MERGE(chosen)", "LEFT JOIN chosen_source_ids chosen ON chosen.seq=r.seq", "wire_header.report_seq=r.seq", "CAST(CASE WHEN wire_header.submission_id IS NOT NULL THEN wire_header.contains_pii", "WHERE r.public_id <> ''"} {
		if !strings.Contains(query, required) {
			t.Fatalf("fused stream lost normalized/null/source contract: %s", required)
		}
	}
	other := f
	other.Kind, other.Sort, other.Limit, other.Offset = "all", "recurrence", 50, 100
	otherQuery, otherArgs, err := hybridEvidenceInputQuery(other, "[]")
	if err != nil || query != otherQuery || !reflect.DeepEqual(args, otherArgs) {
		t.Fatal("kind/sort/pagination changed the full normalized report universe")
	}
}

var hybridTestColumns = strings.Split("seq,public_id,latitude,longitude,classification,severity,language,brand,analysis_created,kind,excerpt,impact,relationship,version,media,stored_id,report_ts,raw_type,raw_id,fetcher,eii_source,eii_id,eii_url,eii_published,eii_created,wire_id,wire_loaded,wire_observed,wire_submitted,wire_actor,wire_channel,node0,node1,node2,node3,node4,node5,node6,node7,node8,node9,node10,node11,node12,node13,node14", ",")

func hybridTestRow(seq int) []driver.Value {
	id := strconv.Itoa(seq)
	row := []driver.Value{seq, "r_test_" + id, 0, 0, "digital", 0.7, "fr", "claude", "2026-10-15 00:00:00.000000", "firsthand", "1", "1", "affected", 1, false,
		"stored_" + id, "2026-10-15 00:00:00.000000", nil, nil, "test", "custom", id, "https://example.test/report/" + id, "2026-10-14 12:00:00.123456", "2026-10-15 00:00:00.000000", nil, false, nil, nil, "", ""}
	return append(row, make([]driver.Value, 15)...)
}

func TestHybridEvidenceScanReuseOwnsNullableValuesAndWireDocuments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, absent, last := hybridTestRow(42), hybridTestRow(43), hybridTestRow(44)
	first[16], first[25], first[26], first[27], first[28], first[42] = "2026-01-01 00:00:00.000000", "private-first", true, "2026-01-01 00:00:00.000000", "2026-01-02 00:00:00.000000", []byte("true")
	absent[5], absent[6], absent[8] = nil, nil, nil
	last[25], last[26], last[27], last[28], last[42] = "public-last", true, "2026-10-01 00:00:00.000000", "2026-10-02 00:00:00.000000", []byte("false")
	mock.ExpectBegin()
	mock.ExpectQuery("WITH identity_scope").WillReturnRows(sqlmock.NewRows(hybridIdentityTestColumns))
	mock.ExpectQuery("WITH preferred").WillReturnRows(sqlmock.NewRows(hybridTestColumns).AddRow(first...).AddRow(absent...).AddRow(last...))
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
	all, selected, _, inputs, err := loadHybridEvidenceInputs(context.Background(), tx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Seq != 42 || all[1].Seq != 43 || all[2].Seq != 44 || !all[1].SeverityNull || !selected[1].languageNull || selected[1].created.Valid || !selected[0].created.Valid {
		t.Fatal("reused record or nullable analysis scratch changed previously retained rows")
	}
	if *inputs[0].ReportTS != "2026-01-01 00:00:00.000000" || *inputs[0].EiiID != "42" || *inputs[2].EiiID != "44" || inputs[0].WireObserved == nil || *inputs[0].WireObserved != "2026-01-01 00:00:00.000000" {
		t.Fatal("retained nullable source fields alias reused scan scratch")
	}
	if len(inputs[1].WireReport) != 0 || len(inputs[1].WireProvenance) != 0 || len(inputs[1].WireExtensions) != 0 || inputs[1].WireObserved != nil || inputs[1].WireSubmitted != nil {
		t.Fatal("an absent Wire row inherited the preceding document or dates")
	}
	for i, want := range map[int]bool{0: true, 2: false} {
		var report struct {
			Privacy struct {
				ContainsPII bool `json:"contains_pii"`
			} `json:"privacy"`
		}
		if err := json.Unmarshal(inputs[i].WireReport, &report); err != nil || report.Privacy.ContainsPII != want {
			t.Fatal("retained typed Wire documents alias the next scan's node buffer")
		}
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
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
	query, args, err := hybridEvidenceInputQuery(f, "[]")
	if err != nil {
		t.Fatal(err)
	}
	values := make([]driver.Value, len(args))
	for i := range args {
		values[i] = args[i]
	}
	first, winner, unrelated := hybridTestRow(42), hybridTestRow(43), hybridTestRow(44)
	first[16], first[5], first[6], first[8] = nil, nil, nil, nil
	winner[16] = nil
	published := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	first[23] = "2026-10-14 11:00:00.123456"
	unrelated[9], unrelated[13] = "unknown", 0
	rows := sqlmock.NewRows(hybridTestColumns).AddRow(first...).AddRow(winner...).AddRow(unrelated...)
	mock.ExpectBegin()
	mock.ExpectQuery("WITH identity_scope").WillReturnRows(sqlmock.NewRows(hybridIdentityTestColumns))
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(values...).WillReturnRows(rows)
	normalizedColumns := strings.Split("seq,platform,url,published,observed,ingested,missing,actor,fetcher,private,key,external,channel,generation,account,null_time", ",")
	normalized := sqlmock.NewRows(normalizedColumns)
	for _, seq := range []int{42, 43, 44} {
		var added driver.Value
		if seq == 44 {
			added = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
		}
		pub := published
		if seq == 42 {
			pub = pub.Add(-time.Hour)
		}
		normalized.AddRow(seq, "custom", fmt.Sprintf("https://example.test/report/%d", seq), pub, nil, added, false, "", "test", false, fmt.Sprintf("url:example.test/report/%d", seq), true, "", "", "", seq != 44)
	}
	mock.ExpectQuery("WITH source_rows AS").WillReturnRows(normalized)
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
			mock.ExpectQuery("WITH identity_scope").WillReturnRows(sqlmock.NewRows(hybridIdentityTestColumns))
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

func TestHybridEvidenceArchiveInputsRemainNativeWithoutWireDocuments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row := hybridTestRow(42)
	digest := fmt.Sprintf("%x", sha1.Sum([]byte("reddit:t3_abc")))[:32]
	row[5], row[6], row[8], row[14] = nil, nil, nil, true
	row[15], row[16], row[20], row[21], row[22], row[23] = "reddit_t3_abc", "2026-03-01 00:00:00.000000", "reddit_archive", digest, nil, nil
	mock.ExpectBegin()
	mock.ExpectQuery("WITH identity_scope").WillReturnRows(sqlmock.NewRows(hybridIdentityTestColumns))
	mock.ExpectQuery("WITH preferred").WillReturnRows(sqlmock.NewRows(hybridTestColumns).AddRow(row...))
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
	all, selected, _, inputs, err := loadHybridEvidenceInputs(context.Background(), tx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].ReportID != "reddit_t3_abc" || inputs[0].RawSourceType != nil || inputs[0].RawSourceID != nil || inputs[0].EiiURL != nil || len(inputs[0].WireReport) != 0 || len(inputs[0].WireProvenance) != 0 || len(inputs[0].WireExtensions) != 0 {
		t.Fatal("raw identity/nulls or absent Wire documents changed")
	}
	// Any unexpected SQL normalization query fails the mock: this exact trusted
	// archive shape must use the already-tested native path.
	if err := normalizeHybridEvidenceInputs(context.Background(), tx, all, inputs); err != nil {
		t.Fatal(err)
	}
	if all[0].SourceKey != "source:reddit_archive:test:"+digest || all[0].PublicID != "r_test_42" || !all[0].HasOriginalMedia || all[0].Excerpt != "1" || !all[0].SeverityNull || !selected[0].languageNull || selected[0].created.Valid || all[0].PublishedAt == nil || all[0].PublishedAt.Month() != time.March || all[0].IngestedAt.Month() != time.October {
		t.Fatal("normalization overwrote selected metadata or changed archive dates")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceWireNodeAssemblyPreservesExactJSONTypesAndPresence(t *testing.T) {
	nodes := make([][]byte, 15)
	nodes[0], nodes[1], nodes[2], nodes[6], nodes[8] = []byte(`"github"`), []byte(`null`), []byte(`"__non_http_source_uri__"`), []byte(`null`), []byte(`7`)
	nodes[11], nodes[12], nodes[13], nodes[14] = []byte(`false`), []byte(`0`), []byte(`"human_submission"`), []byte(`false`)
	var s evidenceSourceNormalizationRow
	if err := assembleEvidenceWireNodes(&s, nodes); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Digital struct {
			Platform json.RawMessage            `json:"platform"`
			Legacy   json.RawMessage            `json:"legacy_source"`
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"digital_context"`
		Privacy map[string]json.RawMessage `json:"privacy"`
	}
	if err := json.Unmarshal(s.WireReport, &report); err != nil {
		t.Fatal(err)
	}
	if string(report.Digital.Legacy) != "null" || string(report.Digital.Metadata["source_published_at"]) != "null" || string(report.Digital.Metadata["author_username"]) != "7" || report.Digital.Metadata["author_did"] != nil || string(report.Privacy["contains_pii"]) != "false" || string(report.Privacy["requires_redaction"]) != "0" || !strings.Contains(string(s.WireExtensions), "false") {
		t.Fatal("Wire SQL NULL/JSON null/boolean/number types changed")
	}
	var present evidenceSourceNormalizationRow
	if err := assembleEvidenceWireNodes(&present, make([][]byte, 15)); err != nil || len(present.WireReport) == 0 || len(present.WireProvenance) == 0 || len(present.WireExtensions) == 0 {
		t.Fatal("a present all-NULL Wire projection must remain a present document")
	}
	nodes[0] = []byte(`{`)
	if err := assembleEvidenceWireNodes(&s, nodes); err == nil {
		t.Fatal("invalid projected JSON must fail closed")
	}
}
