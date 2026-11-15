package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func counterBounds(mock sqlmock.Sqlmock, low, high int) {
	mock.ExpectQuery(`SELECT COALESCE\(MIN\(seq\), 0\), COALESCE\(MAX\(seq\), 0\)`).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(low, high))
}

func counterPage(mock sqlmock.Sqlmock, cursor, high, batch, total, physical, digital int, last any) {
	mock.ExpectQuery(regexp.QuoteMeta(reportCounterPageSQL)).WithArgs(cursor, high, batch).
		WillReturnRows(sqlmock.NewRows([]string{"total", "physical", "digital", "last"}).AddRow(total, physical, digital, last))
}

func TestReportCounterReconcilesOlderSequencesAndPublishesOnlyCompletePass(t *testing.T) {
	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbc.Close()
	previous := reportCountSnapshot{totalReports: 99, physicalCount: 90, digitalCount: 9, lastCountedSeq: 100, lastUpdated: time.Unix(100, 0).UTC()}
	cache := &reportCountCache{reportCountSnapshot: previous, initialized: true}
	counterBounds(mock, 1, 100) // MAX has not increased since the previous pass.
	counterPage(mock, 0, 100, 2, 2, 1, 1, 3)
	counterPage(mock, 3, 100, 2, 0, 0, 0, 95) // Invalid-only groups still advance.
	counterPage(mock, 95, 100, 2, 1, 1, 0, 100)
	mock.ExpectExec(`INSERT INTO report_counts_total`).WithArgs(3, 2, 1, 100, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	pauses := 0
	err = refreshCounterCache(context.Background(), dbc, cache, 2, func(context.Context) error {
		pauses++
		if !reflect.DeepEqual(cache.reportCountSnapshot, previous) {
			t.Fatal("partial pass became visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pauses != 2 || cache.totalReports != 3 || cache.physicalCount != 2 || cache.digitalCount != 1 || cache.lastCountedSeq != 100 || !cache.lastUpdated.After(previous.lastUpdated) {
		t.Fatalf("incorrect complete snapshot: pauses=%d snapshot=%+v", pauses, cache.reportCountSnapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportCounterFailureRetainsPreviousTotalsAndFreshness(t *testing.T) {
	for _, failure := range []string{"page", "persist", "pause", "nonadvancing"} {
		t.Run(failure, func(t *testing.T) {
			dbc, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer dbc.Close()
			previous := reportCountSnapshot{totalReports: 77, physicalCount: 66, digitalCount: 11, lastCountedSeq: 100, lastUpdated: time.Unix(100, 0).UTC()}
			cache := &reportCountCache{reportCountSnapshot: previous, initialized: true}
			counterBounds(mock, 1, 100)
			var pause func(context.Context) error
			switch failure {
			case "page":
				counterPage(mock, 0, 100, 2, 2, 2, 0, 3)
				mock.ExpectQuery(regexp.QuoteMeta(reportCounterPageSQL)).WithArgs(3, 100, 2).WillReturnError(context.DeadlineExceeded)
			case "persist":
				counterPage(mock, 0, 100, 2, 2, 2, 0, 100)
				mock.ExpectExec(`INSERT INTO report_counts_total`).WillReturnError(errors.New("write unavailable"))
			case "pause":
				counterPage(mock, 0, 100, 2, 2, 2, 0, 3)
				pause = func(context.Context) error { return context.Canceled }
			case "nonadvancing":
				counterPage(mock, 0, 100, 2, 0, 0, 0, 0)
			}
			if err := refreshCounterCache(context.Background(), dbc, cache, 2, pause); err == nil {
				t.Fatal("failed pass reported success")
			}
			if !cache.initialized || !reflect.DeepEqual(cache.reportCountSnapshot, previous) {
				t.Fatalf("failed pass changed the previous snapshot: %+v", cache)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReportCounterEmptyHistoryIsACompleteZeroSnapshot(t *testing.T) {
	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbc.Close()
	counterBounds(mock, 0, 0)
	counterPage(mock, -1, 0, 2, 0, 0, 0, nil)
	mock.ExpectExec(`INSERT INTO report_counts_total`).WithArgs(0, 0, 0, 0, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	cache := &reportCountCache{}
	if err := refreshCounterCache(context.Background(), dbc, cache, 2, nil); err != nil {
		t.Fatal(err)
	}
	if !cache.initialized || cache.totalReports != 0 || cache.physicalCount != 0 || cache.digitalCount != 0 || cache.lastUpdated.IsZero() {
		t.Fatalf("empty history did not publish a complete zero snapshot: %+v", cache)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportCounterLoadPreservesStoredFreshnessAndRequestsDoNoDBWork(t *testing.T) {
	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbc.Close()
	previous := countCache
	countCache = &reportCountCache{}
	t.Cleanup(func() { countCache = previous })
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		GetValidReportsCount(c)
		return recorder
	}
	if got := request(); got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") != "30" {
		t.Fatalf("unknown counts were presented as a real zero: %d %s", got.Code, got.Body.String())
	}
	mock.ExpectQuery(`SELECT total_valid, physical_valid, digital_valid, last_counted_seq,`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "physical", "digital", "seq", "updated"}).AddRow(8, 5, 3, 100, 1000))
	if !tryLoadCounterCacheFromDB(dbc) || !countCache.lastUpdated.Equal(time.Unix(1000, 0).UTC()) {
		t.Fatal("loading a stale snapshot fabricated current freshness")
	}
	for i := 0; i < 2; i++ {
		if got := request(); got.Code != http.StatusOK {
			t.Fatalf("cached request failed: %d %s", got.Code, got.Body.String())
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportCounterWorkBoundsAndCancellation(t *testing.T) {
	dbc, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbc.Close()
	for _, size := range []int{0, -1, reportCounterBatchSize + 1} {
		if _, err := reconcileReportCounts(context.Background(), dbc, size, nil); err == nil {
			t.Fatalf("invalid batch size %d was accepted", size)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pauseCounterPage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause ignored cancellation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
