package database

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvidenceProjectionPreservesNullZeroPrecisionAndTimezone(t *testing.T) {
	zero := time.Time{}
	date := time.Date(2026, 10, 15, 16, 17, 18, 123456000, time.FixedZone("test", 3600))
	r := evidenceRecord{Seq: 42, PublishedAt: &zero, ObservedAt: &date, IngestedAt: zero, IngestionTimeMissing: true}
	cached := evidenceProjectionRecord(r)
	if *cached.SourceTimestamp != "0000-00-00 00:00:00.000000" || *cached.ObservedAt != "2026-10-15 15:17:18.123456" || *cached.TS != "0000-00-00 00:00:00.000000" || cached.IngestedAtNull {
		t.Fatal("cache serialization changed zero, precision or UTC time")
	}
	r.IngestedAtNull, r.PublishedAt = true, nil
	null := evidenceProjectionRecord(r)
	if null.TS != nil || null.SourceTimestamp != nil || !null.IngestedAtNull || cached.TS == nil {
		t.Fatal("SQL NULL was conflated with a zero date or retained date storage was reused")
	}
}

func TestEvidenceProjectionRefreshRejectsUnboundedOrAmbiguousBatches(t *testing.T) {
	for _, ids := range [][]int{nil, {0}, {-1}, {1, 1}, make([]int, 201)} {
		if _, err := projectionRefreshIDs(ids); err == nil {
			t.Fatal("accepted an invalid or unbounded refresh")
		}
	}
	input := []int{42, 1, 7}
	got, err := projectionRefreshIDs(input)
	if err != nil || !reflect.DeepEqual(got, []int{1, 7, 42}) || !reflect.DeepEqual(input, []int{42, 1, 7}) {
		t.Fatal("generation lock order was not sorted independently of the caller")
	}
}

func TestEvidenceProjectionWorkerStopsWithoutDatabaseWorkWhenCanceled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	(&Database{db: db}).RunEvidenceSourceProjectionWorker(ctx)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
