package reportmeta

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func expectMigration(mock sqlmock.Sqlmock, triggersExist bool) {
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(31536000))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS report_metadata_projection`).WillReturnResult(sqlmock.NewResult(0, 0))
	for _, trigger := range metadataTriggers() {
		rows := sqlmock.NewRows([]string{"table", "timing", "event", "body"})
		if triggersExist {
			rows.AddRow("reports", "AFTER", trigger.event, trigger.body)
		}
		mock.ExpectQuery(`SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT FROM INFORMATION_SCHEMA.TRIGGERS`).WithArgs(trigger.name).WillReturnRows(rows)
		if !triggersExist {
			mock.ExpectExec("CREATE TRIGGER " + trigger.name).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(31536000).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMigrationRejectsForeignExistingTrigger(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS report_metadata_projection`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT FROM INFORMATION_SCHEMA.TRIGGERS`).WithArgs("reports_metadata_after_insert").WillReturnRows(sqlmock.NewRows([]string{"table", "timing", "event", "body"}).AddRow("reports", "AFTER", "INSERT", "SET @unrelated=1"))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := Migrate(context.Background(), db); err == nil {
		t.Fatal("silently accepted foreign existing trigger")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationCreatesOnlyProjectionAndMissingTriggers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectMigration(mock, false)
	expectMigration(mock, true)
	for i := 0; i < 2; i++ {
		if err := Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRestoresSessionLimitOnDDLFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"timeout"}).AddRow(60))
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	failure := errors.New("metadata lock deadline")
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS report_metadata_projection`).WillReturnError(failure)
	mock.ExpectExec(`SET SESSION lock_wait_timeout = \?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := Migrate(context.Background(), db); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
