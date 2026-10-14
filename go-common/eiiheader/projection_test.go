package eiiheader

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const strictMode = "STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,NO_ENGINE_SUBSTITUTION"

func expectSession(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT @@SESSION.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"mode", "metadata", "row"}).AddRow(strictMode, 60, 50))
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs("STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION", 5, 5).WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectRestore(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs(strictMode, 60, 50).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestCopyModePreservesStrictValidationWhileAllowingLegacyDates(t *testing.T) {
	if got := copyMode(strictMode); got != "STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION" {
		t.Fatalf("removed unrelated caller validation: %s", got)
	}
	for _, trigger := range projectionTriggers() {
		if strings.Contains(trigger.body, "UPDATE external_ingest_index") || strings.Contains(trigger.body, "ALTER ") {
			t.Fatal("projection trigger changed parent history or schema")
		}
	}
}

func TestMigrationRejectsUnhandledCascadeContractAndRestoresSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectSession(mock)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	expectRestore(mock)
	if err := Migrate(context.Background(), db); err == nil {
		t.Fatal("accepted parent cascades which do not fire triggers")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillIsBoundedLockingAndPreservesNullHeaders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n, err := Backfill(context.Background(), db, nil); err != nil || n != 0 {
		t.Fatal("empty backfill accessed the database")
	}
	if _, err := Backfill(context.Background(), db, make([]int, 201)); err == nil {
		t.Fatal("accepted an unbounded backfill")
	}
	expectSession(mock)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO external_ingest_header_projection .* SELECT seq,source,external_id,source_url,source_timestamp,created_at FROM external_ingest_index FORCE INDEX\(idx_seq\) WHERE seq IN \(\?,\?\) FOR SHARE ON DUPLICATE KEY UPDATE`).WithArgs(10, 11).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	expectRestore(mock)
	if n, err := Backfill(context.Background(), db, []int{10, 11}); err != nil || n != 2 {
		t.Fatalf("bounded locking copy failed: %d %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillFailureRollsBackAndRestoreFailureDiscardsConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectSession(mock)
	mock.ExpectBegin()
	copyFailure, restoreFailure := errors.New("copy failed"), errors.New("reset failed")
	mock.ExpectExec(`INSERT INTO external_ingest_header_projection`).WillReturnError(copyFailure)
	mock.ExpectRollback()
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs(strictMode, 60, 50).WillReturnError(restoreFailure)
	if _, err := Backfill(context.Background(), db, []int{10}); !errors.Is(err, copyFailure) || !errors.Is(err, restoreFailure) {
		t.Fatalf("lost copy/reset failure: %v", err)
	}
	if stats := db.Stats(); stats.OpenConnections != 0 || stats.Idle != 0 {
		t.Fatal("returned changed session settings to the pool")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
