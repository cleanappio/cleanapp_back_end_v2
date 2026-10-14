package database

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvidenceStoreMigrationUpgradesLegacyAppliedColumnStep(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS go_schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
	steps := sqlmock.NewRows([]string{"step_id"})
	for _, id := range []string{"0001_report_analysis_table", "0002_report_analysis_columns_and_indexes", "0003_osm_cache_table", "0004_brand_contacts_table", "0005_seed_known_contacts", "0006_report_evidence"} {
		steps.AddRow(id)
	}
	mock.ExpectQuery(`SELECT step_id FROM go_schema_migrations WHERE service_name = \?`).WithArgs("report-analyze-pipeline").WillReturnRows(steps)
	// Only the independent evidence table is created; no report-analysis ALTER,
	// new reports index, or baseline rebuild is expected in this upgrade.
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS report_evidence`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO go_schema_migrations`).WithArgs("report-analyze-pipeline", "0007_report_evidence_store", "ensure separate per-report evidence storage").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := RunMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
