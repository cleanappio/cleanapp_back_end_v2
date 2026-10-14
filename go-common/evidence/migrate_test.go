package evidence

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"strings"
	"testing"
)

func TestExplicitMigrationBatchesMissingColumnsAndIsIdempotent(t *testing.T) {
	columns := []struct{ name, definition string }{
		{"report_kind", "VARCHAR(16) NOT NULL DEFAULT 'unknown'"},
		{"evidence_excerpt", "TEXT NULL"},
		{"reported_impact", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"classification_reason", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"entity_relationship", "VARCHAR(16) NOT NULL DEFAULT 'unknown'"},
		{"evidence_version", "INT NOT NULL DEFAULT 0"},
	}
	for _, tt := range []struct {
		name            string
		existingColumns int
		existingIndexes bool
	}{
		{"fresh schema one column alter", 0, false},
		{"partial schema missing subset only", 2, false},
		{"rerun no DDL", len(columns), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			additions := []string{}
			for i, column := range columns {
				count := 0
				if i < tt.existingColumns {
					count = 1
				} else {
					additions = append(additions, "ADD COLUMN "+column.name+" "+column.definition)
				}
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.COLUMNS`).WithArgs(column.name).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
			}
			// The metadata checks all precede one ALTER. On a FULLTEXT table, six
			// separate ADD COLUMN statements can otherwise repeat an expensive rebuild.
			if len(additions) > 0 {
				mock.ExpectExec(regexp.QuoteMeta("ALTER TABLE report_analysis " + strings.Join(additions, ", "))).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			indexCount := 0
			if tt.existingIndexes {
				indexCount = 1
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(indexCount))
			if !tt.existingIndexes {
				mock.ExpectExec(`ALTER TABLE report_analysis ADD INDEX idx_report_analysis_brand_kind_language`).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(indexCount))
			if !tt.existingIndexes {
				mock.ExpectExec(`ALTER TABLE reports ADD INDEX idx_reports_evidence_ingested`).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			if err := Migrate(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestColumnBatchFailureDoesNotContinueToIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, column := range []string{"report_kind", "evidence_excerpt", "reported_impact", "classification_reason", "entity_relationship", "evidence_version"} {
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.COLUMNS`).WithArgs(column).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	mock.ExpectExec(`ALTER TABLE report_analysis ADD COLUMN report_kind.*ADD COLUMN evidence_version`).WillReturnError(errors.New("DDL failed"))
	if err := Migrate(context.Background(), db); err == nil || !strings.Contains(err.Error(), "DDL failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
