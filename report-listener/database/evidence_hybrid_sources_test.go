package database

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

var hybridIdentityTestColumns = []string{"kind", "seq", "source", "external_id", "wire_id"}

func TestHybridSourceParameterRejectsOversizedCohortWithoutPartialJSON(t *testing.T) {
	source, id, wire := strings.Repeat("s", 64), strings.Repeat("i", 255), strings.Repeat("w", 64)
	choice := &hybridEvidenceSourceChoice{Seq: 42, Source: &source, ExternalID: &id, WireID: &wire}
	selected := make([]*hybridEvidenceSourceChoice, 25000)
	for i := range selected {
		selected[i] = choice
	}
	encoded, err := encodeHybridEvidenceSourceChoices(selected)
	if err == nil || encoded != "" {
		t.Fatal("oversized source identities produced a usable or truncated parameter")
	}
}

func TestHybridSourceIdentityScopeIsSharedCoveredAndUnfiltered(t *testing.T) {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Kind: "coverage", Language: "fr", Limit: 1, Offset: 100})
	query, args := hybridEvidenceSourceIdentityQuery(f)
	want := make([]interface{}, len(f.Brands))
	for i, brand := range f.Brands {
		want[i] = brand
	}
	if !reflect.DeepEqual(args, want) || strings.Count(query, "FROM report_analysis") != 1 || strings.Count(query, "FROM identity_scope p STRAIGHT_JOIN") != 2 {
		t.Fatal("the two covering identity streams must share the complete distinct brand seq scope")
	}
	for _, forbidden := range []string{"ORDER BY", "ROW_NUMBER", "LATERAL", "wire_evidence_projection", "source_url", "submitted_at", "report_kind", "report_status", "reports_owners", "LIMIT"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("covered identity scan introduced wide, ordered, or filtered work: %s", forbidden)
		}
	}
}

func TestHybridSourceChoicesResolveOnlyDuplicateKindsAndKeepSQLWinners(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
	// 10 has one source of each kind: these are two independent singletons.
	// 20 is EII-only, 30 Wire-only. The unordered first tuples of 40 lose to
	// the database's native ordered tuples, which Go must never compare itself.
	rows := sqlmock.NewRows(hybridIdentityTestColumns).
		AddRow(1, 40, "z", "z-id", nil).
		AddRow(1, 10, "singleton", "Keep CASE ", nil).
		AddRow(1, 20, "only-external", "e20", nil).
		AddRow(1, 40, "é", "Native CASE ", nil).
		AddRow(2, 40, nil, nil, "older").
		AddRow(2, 10, nil, nil, "one-wire").
		AddRow(2, 30, nil, nil, "wire-only").
		AddRow(2, 40, nil, nil, "latest-private")
	mock.ExpectBegin()
	mock.ExpectQuery("WITH identity_scope").WillReturnRows(rows)
	mock.ExpectQuery("SELECT /\\*\\+ MAX_EXECUTION_TIME\\(8000\\) \\*/ e.seq,e.source,e.external_id").WithArgs("[40]").WillReturnRows(
		sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(40, "é", "Native CASE ").AddRow(40, "z", "z-id"))
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY ids.seq,CASE WHEN h.submission_id IS NOT NULL THEN h.submitted_at ELSE fallback.submitted_at END DESC")).WithArgs("[40]").WillReturnRows(
		sqlmock.NewRows([]string{"seq", "id"}).AddRow(40, "latest-private").AddRow(40, "older"))
	mock.ExpectRollback()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	encoded, err := loadHybridEvidenceSourceChoices(context.Background(), tx, f)
	if err != nil {
		t.Fatal(err)
	}
	var selected [][4]interface{}
	if err := json.Unmarshal([]byte(encoded), &selected); err != nil {
		t.Fatal(err)
	}
	if len(selected) != 4 || selected[0][0] != float64(10) || selected[0][2] != "Keep CASE " || selected[0][3] != "one-wire" || selected[1][3] != nil || selected[2][1] != nil || selected[3][1] != "é" || selected[3][2] != "Native CASE " || selected[3][3] != "latest-private" {
		t.Fatalf("singleton independence, SQL winner, NULL source, or identity bytes changed: %s", encoded)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHybridSourceChoicesFailClosedOnPartialIdentityOrDuplicateReads(t *testing.T) {
	for _, mode := range []string{"partial_identity", "stream_error", "missing_duplicate", "duplicate_error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows := sqlmock.NewRows(hybridIdentityTestColumns).AddRow(1, 42, "a", "one", nil)
			switch mode {
			case "partial_identity":
				rows.AddRow(1, 42, "a", nil, nil)
			case "stream_error":
				rows.AddRow(2, 42, nil, nil, "wire").RowError(1, errors.New("interrupted identity read"))
			default:
				rows.AddRow(1, 42, "b", "two", nil)
			}
			mock.ExpectBegin()
			mock.ExpectQuery("WITH identity_scope").WillReturnRows(rows)
			if mode == "missing_duplicate" {
				mock.ExpectQuery("ORDER BY ids.seq,e.source,e.external_id").WithArgs("[42]").WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "id"}).AddRow(42, "a", "one"))
			} else if mode == "duplicate_error" {
				mock.ExpectQuery("ORDER BY ids.seq,e.source,e.external_id").WithArgs("[42]").WillReturnError(errors.New("duplicate read interrupted"))
			}
			mock.ExpectRollback()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude", Kind: "all"})
			encoded, err := loadHybridEvidenceSourceChoices(context.Background(), tx, f)
			if err == nil || encoded != "" {
				t.Fatal("incomplete identity reads must not produce a usable selected cohort")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
