package reportmeta

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBackfillBoundsAndReconcilesOnlySelectedReports(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := Backfill(context.Background(), db, nil); err != nil || count != 0 {
		t.Fatalf("empty batch: count=%d err=%v", count, err)
	}
	if _, err := Backfill(context.Background(), db, make([]int, 201)); err == nil {
		t.Fatal("accepted unbounded batch")
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO report_metadata_projection .* SELECT .* FROM reports WHERE seq IN \(\?,\?\) FOR SHARE ON DUPLICATE KEY UPDATE`).WithArgs(10, 11).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if count, err := Backfill(context.Background(), db, []int{10, 11}); err != nil || count != 1 {
		t.Fatalf("copy: count=%d err=%v", count, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillRollsBackFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("copy failed")
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO report_metadata_projection`).WillReturnError(failure)
	mock.ExpectRollback()
	if _, err := Backfill(context.Background(), db, []int{10}); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
