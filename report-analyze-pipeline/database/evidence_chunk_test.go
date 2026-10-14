package database

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvidenceChunkRejectsUnboundedOrInvalidRangesAndBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Database{db: db}
	for _, test := range []struct {
		low, high int64
		limit     int
	}{{-1, 1, 10}, {5, 4, 10}, {0, 501, 10}, {0, 500, 0}, {0, 500, 51}} {
		if _, err := store.GetPendingEvidenceChunk(context.Background(), test.low, test.high, test.limit); err == nil {
			t.Fatalf("invalid chunk accepted: %+v", test)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceChunkUsesExactNumericRangeAndConfiguredLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Database{db: db}
	mock.ExpectQuery(regexp.QuoteMeta(pendingEvidenceChunkQuery)).WithArgs(int64(1000), int64(1500), 2).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(1499, "grok").AddRow(1100, "claude"))
	got, err := store.GetPendingEvidenceChunk(context.Background(), 1000, 1500, 2)
	if err != nil || len(got) != 2 || got[0].Seq != 1499 || got[1].Seq != 1100 {
		t.Fatalf("wrong bounded batch: %+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
