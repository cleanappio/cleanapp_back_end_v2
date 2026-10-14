package database

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func evidenceIndexRows(columns []string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"column", "prefix", "non_unique", "type", "visible", "direction"})
	for _, column := range columns {
		rows.AddRow(column, nil, 1, "BTREE", "YES", "A")
	}
	return rows
}

func TestEvidenceBrandIndexDiscardsSessionIfLockLimitCannotBeRestored(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COLUMN_NAME,SUB_PART,NON_UNIQUE,INDEX_TYPE,IS_VISIBLE`).WithArgs("idx_ra_brand_valid_class_core").WillReturnRows(evidenceIndexRows([]string{"brand_name", "is_valid", "classification", "seq", "language", "created_at", "severity_level"}))
	failure := errors.New("session reset failed")
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnError(failure)
	mock.ExpectClose()
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats := db.Stats(); stats.Idle != 0 || stats.OpenConnections != 0 {
		t.Fatal("session with altered lock limit was returned to the pool")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceBrandIndexCreatesOnlyStrictOnlineSecondaryIndex(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COLUMN_NAME,SUB_PART,NON_UNIQUE,INDEX_TYPE,IS_VISIBLE,COLLATION FROM INFORMATION_SCHEMA.STATISTICS`).WithArgs("idx_ra_brand_valid_class_core").WillReturnRows(evidenceIndexRows(nil))
	mock.ExpectExec(`ALTER TABLE report_analysis ADD INDEX idx_ra_brand_valid_class_core \(brand_name,is_valid,classification,seq,language,created_at,severity_level\), ALGORITHM=INPLACE, LOCK=NONE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceBrandIndexVerifiesExistingDefinitionWithoutDDL(t *testing.T) {
	for _, valid := range []bool{true, false} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
		mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
		columns := []string{"brand_name", "is_valid", "classification", "seq", "language", "created_at", "severity_level"}
		if !valid {
			columns[2] = "language"
		}
		mock.ExpectQuery(`SELECT COLUMN_NAME,SUB_PART,NON_UNIQUE,INDEX_TYPE,IS_VISIBLE,COLLATION FROM INFORMATION_SCHEMA.STATISTICS`).WithArgs("idx_ra_brand_valid_class_core").WillReturnRows(evidenceIndexRows(columns))
		mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
		err = ensureEvidenceBrandCoreIndex(context.Background(), db)
		if (err == nil) != valid {
			t.Fatalf("valid=%t unexpected migration error: %v", valid, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestEvidenceBrandIndexNeverFallsBackToCopy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COLUMN_NAME,SUB_PART,NON_UNIQUE,INDEX_TYPE,IS_VISIBLE,COLLATION FROM INFORMATION_SCHEMA.STATISTICS`).WithArgs("idx_ra_brand_valid_class_core").WillReturnRows(evidenceIndexRows(nil))
	failure := errors.New("online algorithm unavailable")
	mock.ExpectExec(`ALTER TABLE report_analysis`).WillReturnError(failure)
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
