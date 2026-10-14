package database

import (
	"context"
	"database/sql"
	"errors"
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
