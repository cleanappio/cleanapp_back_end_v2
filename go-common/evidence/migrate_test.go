package evidence

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestExplicitMigrationIsIdempotent(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "first run", true: "rerun"}[exists], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			count := 0
			if exists {
				count = 1
			}
			for _, column := range []string{"report_kind", "evidence_excerpt", "reported_impact", "classification_reason", "entity_relationship", "evidence_version"} {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.COLUMNS`).WithArgs(column).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if !exists {
					mock.ExpectExec(`ALTER TABLE report_analysis ADD COLUMN ` + column).WillReturnResult(sqlmock.NewResult(0, 0))
				}
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
			if !exists {
				mock.ExpectExec(`ALTER TABLE report_analysis ADD INDEX idx_report_analysis_brand_kind_language`).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
			if !exists {
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
