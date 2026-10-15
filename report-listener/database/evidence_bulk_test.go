package database

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvidenceBulkFailureCannotReturnPartialExactCounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT seq,language,created_at").WillReturnRows(sqlmock.NewRows([]string{"seq", "language", "created_at", "brand_name", "classification", "severity", "requested", "english", "weight", "created_literal"}).AddRow(1, "en", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), "openai", "digital", 0.5, 1, 1, []byte("en"), "2026-10-14 00:00:00.000000"))
	mock.ExpectQuery("SELECT ids.seq").WillReturnError(errors.New("metadata deadline exceeded"))
	mock.ExpectRollback()
	page, err := (&Database{db: db}).GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "openai"})
	if err == nil || page.Total != 0 || len(page.Reports) != 0 {
		t.Fatal("partial snapshot returned apparently exact counts")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWireCoveredReadUsesTimestampsOnlyForDuplicateGroups(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// SQL's 0900 collation orders é ahead of z; Go byte sorting would reverse it.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ws.submission_id,ws.report_seq FROM ` + evidenceIDsTable + ` STRAIGHT_JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.submission_id`)).WithArgs("[1,2]").WillReturnRows(sqlmock.NewRows([]string{"id", "seq"}).AddRow("é-single", 1).AddRow("z-new", 2).AddRow("z-old", 2))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ws.submission_id,ws.report_seq FROM ` + evidenceIDsTable + ` JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.report_seq,ws.submitted_at DESC`)).WithArgs("[2]").WillReturnRows(sqlmock.NewRows([]string{"id", "seq"}).AddRow("z-old", 2).AddRow("z-new", 2))
	columns := []string{"seq", "observed", "submitted", "actor", "channel", "p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12", "generation", "flag"}
	mock.ExpectQuery("SELECT ids.seq,DATE_FORMAT.*LEFT JOIN wire_evidence_projection h ON h.submission_id=ids.id AND h.report_seq=ids.seq.*LEFT JOIN wire_submissions_raw ws ON ws.submission_id=CASE WHEN h.submission_id IS NULL THEN ids.id ELSE NULL END AND ws.report_seq=ids.seq").WithArgs(`[{"id":"é-single","seq":1},{"id":"z-old","seq":2}]`).WillReturnRows(sqlmock.NewRows(columns).AddRow(1, nil, nil, "human", "web", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).AddRow(2, nil, nil, "machine", "wire", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	sources := map[int]evidenceSourceNormalizationRow{1: {Seq: 1}, 2: {Seq: 2}}
	if err := loadEvidenceWireSources(context.Background(), tx, []int{1, 2}, sources); err != nil {
		t.Fatal(err)
	}
	if sources[1].WireActor != "human" || sources[2].WireActor != "machine" {
		t.Fatal("selected Wire records changed")
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWireSingleSubmissionDoesNotReadTimestampHeader(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT ws.submission_id,ws.report_seq.*STRAIGHT_JOIN").WithArgs("[1]").WillReturnRows(sqlmock.NewRows([]string{"id", "seq"}).AddRow("single", 1))
	mock.ExpectQuery("SELECT ids.seq,DATE_FORMAT.*LEFT JOIN wire_evidence_projection h ON h.submission_id=ids.id AND h.report_seq=ids.seq.*LEFT JOIN wire_submissions_raw ws ON ws.submission_id=CASE WHEN h.submission_id IS NULL THEN ids.id ELSE NULL END AND ws.report_seq=ids.seq").WithArgs(`[{"id":"single","seq":1}]`).WillReturnRows(sqlmock.NewRows([]string{"seq", "observed", "submitted", "actor", "channel", "p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12", "generation", "flag"}).AddRow(1, nil, nil, "human", "web", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	if err := loadEvidenceWireSources(context.Background(), tx, []int{1}, map[int]evidenceSourceNormalizationRow{1: {Seq: 1}}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectRollback()
	tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidencePreferredAnalysisPreservesSQLLanguageAndNullOrdering(t *testing.T) {
	requested := evidenceAnalysisCore{Requested: sql.NullInt64{Valid: true, Int64: 1}, English: sql.NullInt64{Valid: true}, LanguageWeight: []byte("fr")}
	english := evidenceAnalysisCore{Requested: sql.NullInt64{Valid: true}, English: sql.NullInt64{Valid: true, Int64: 1}, LanguageWeight: []byte("en")}
	if !preferredEvidenceAnalysis(requested, english) {
		t.Fatal("requested language must precede English")
	}
	newer := english
	newer.Created = sql.NullTime{Valid: true, Time: time.Now()}
	if !preferredEvidenceAnalysis(newer, english) {
		t.Fatal("known newer creation must precede NULL creation")
	}
	nullLanguage := evidenceAnalysisCore{}
	if preferredEvidenceAnalysis(nullLanguage, english) {
		t.Fatal("NULL preference flags must follow a known false/true flag")
	}
}

func TestWireMissingSelectedMetadataFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT ws.submission_id,ws.report_seq.*STRAIGHT_JOIN").WithArgs("[1]").WillReturnRows(sqlmock.NewRows([]string{"id", "seq"}).AddRow("single", 1))
	mock.ExpectQuery("SELECT ids.seq,DATE_FORMAT").WithArgs(`[{"id":"single","seq":1}]`).WillReturnRows(sqlmock.NewRows([]string{"seq", "observed", "submitted", "actor", "channel", "p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12", "generation", "flag"}))
	if err := loadEvidenceWireSources(context.Background(), tx, []int{1}, map[int]evidenceSourceNormalizationRow{1: {Seq: 1}}); err == nil {
		t.Fatal("missing selected Wire metadata silently defaulted privacy/provenance")
	}
	mock.ExpectRollback()
	tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceIndexSourcesOrdersOnlyAmbiguousIdentities(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	covered := `SELECT e.seq,e.source,e.external_id FROM ` + evidenceIDsTable + ` JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq`
	// Most reports have one identity and need no global string/collation sort.
	mock.ExpectQuery(regexp.QuoteMeta(covered) + `$`).WithArgs("[1,2,3]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(3, "z", "last").AddRow(2, "z", "later").AddRow(1, "sole", "one").AddRow(2, "é", "winner"))
	// SQL, not Go byte order, remains authoritative for ambiguous reports.
	mock.ExpectQuery(regexp.QuoteMeta(covered+` ORDER BY e.source,e.external_id`) + `$`).WithArgs("[2]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(2, "é", "winner").AddRow(2, "z", "later"))
	mock.ExpectQuery("SELECT ids.seq,ids.source,ids.id").WithArgs(`[{"seq":1,"source":"sole","id":"one"},{"seq":2,"source":"é","id":"winner"},{"seq":3,"source":"z","id":"last"}]`).WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id", "url", "published", "created"}).AddRow(3, "z", "last", "https://example.test/3", nil, nil).AddRow(2, "é", "winner", "https://example.test/2", nil, nil).AddRow(1, "sole", "one", "https://example.test/1", nil, nil))
	sources := map[int]evidenceSourceNormalizationRow{1: {Seq: 1}, 2: {Seq: 2}, 3: {Seq: 3}}
	if err := loadEvidenceIndexSources(context.Background(), tx, []int{1, 2, 3}, sources); err != nil {
		t.Fatal(err)
	}
	if *sources[2].EiiSource != "é" || *sources[2].EiiID != "winner" || *sources[1].EiiURL != "https://example.test/1" || *sources[3].EiiURL != "https://example.test/3" {
		t.Fatal("source winner or keyed hydration changed")
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceIndexSingletonsNeedNoCollationSort(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	covered := `SELECT e.seq,e.source,e.external_id FROM ` + evidenceIDsTable + ` JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq`
	mock.ExpectQuery(regexp.QuoteMeta(covered) + `$`).WithArgs("[1,2]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(2, "source", "two").AddRow(1, "source", "one"))
	mock.ExpectQuery("SELECT ids.seq,ids.source,ids.id").WithArgs(`[{"seq":1,"source":"source","id":"one"},{"seq":2,"source":"source","id":"two"}]`).WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id", "url", "published", "created"}).AddRow(1, "source", "one", nil, nil, nil).AddRow(2, "source", "two", nil, nil, nil))
	if err := loadEvidenceIndexSources(context.Background(), tx, []int{1, 2}, map[int]evidenceSourceNormalizationRow{}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceIndexMissingWinnerOrHeaderFailsClosed(t *testing.T) {
	for _, missing := range []string{"ambiguous_winner", "header"} {
		t.Run(missing, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			rows := sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(1, "source", "one")
			if missing == "ambiguous_winner" {
				rows.AddRow(1, "source", "two")
			}
			mock.ExpectQuery("SELECT e.seq,e.source,e.external_id").WithArgs("[1]").WillReturnRows(rows)
			if missing == "ambiguous_winner" {
				mock.ExpectQuery("SELECT e.seq,e.source,e.external_id.*ORDER BY e.source,e.external_id").WithArgs("[1]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}))
			} else {
				mock.ExpectQuery("SELECT ids.seq,ids.source,ids.id").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id", "url", "published", "created"}))
			}
			if err := loadEvidenceIndexSources(context.Background(), tx, []int{1}, map[int]evidenceSourceNormalizationRow{}); err == nil {
				t.Fatal("incomplete selected source metadata silently returned exact counts")
			}
			mock.ExpectRollback()
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
