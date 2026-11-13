package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type randomActorHash struct{}

func (randomActorHash) Match(value driver.Value) bool {
	b, ok := value.([]byte)
	if !ok || len(b) != 32 {
		return false
	}
	var nonzero bool
	for _, v := range b {
		nonzero = nonzero || v != 0
	}
	return nonzero
}

func TestPublicAgentActorCannotUseLegacyBearerAuthentication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &Database{db: db}
	// Regression: the ordinary internal-profile helper used a predictable hash.
	// This profile instead has no known credential and active=FALSE even on reuse.
	mock.ExpectExec(`INSERT INTO fetchers.*VALUES \(\?,'Public anonymous agent intake',\?,NULL,FALSE.*ON DUPLICATE KEY UPDATE active=FALSE`).WithArgs("public-agent-anonymous-v1", randomActorHash{}).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := d.EnsurePublicAgentActor(context.Background(), "public-agent-anonymous-v1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicAgentQuotaReservationIsAtomicAndDoesNotCreateIPRowsAfterGlobalLimit(t *testing.T) {
	for _, blocked := range []string{"global", "ip"} {
		t.Run(blocked, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			d := &Database{db: db}
			now := time.Now().UTC()
			minute := now.Truncate(time.Minute)
			day := now.Truncate(24 * time.Hour)
			mock.ExpectBegin()
			for _, key := range []string{"global", "ip"} {
				mock.ExpectExec(`INSERT INTO fetcher_usage_minute`).WithArgs("public", key, minute).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO fetcher_usage_daily`).WithArgs("public", key, day).WillReturnResult(sqlmock.NewResult(0, 1))
				used := 0
				if key == blocked {
					used = 5
				}
				mock.ExpectQuery(`SELECT items FROM fetcher_usage_minute`).WithArgs("public", key, minute).WillReturnRows(sqlmock.NewRows([]string{"items"}).AddRow(used))
				mock.ExpectQuery(`SELECT items FROM fetcher_usage_daily`).WithArgs("public", key, day).WillReturnRows(sqlmock.NewRows([]string{"items"}).AddRow(0))
				if key == blocked {
					break
				}
			}
			// No increments are permitted if any budget failed. A newly inserted IP
			// row is rolled back as well; a global rejection never even touches it.
			mock.ExpectRollback()
			err = d.ConsumeQuotaBudgetsV1(context.Background(), []QuotaBudgetV1{{"public", "global", 5, 50}, {"public", "ip", 5, 50}}, now, 1)
			if !errors.Is(err, ErrQuotaExceeded) {
				t.Fatalf("got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicAgentIntentLockReleasesOnPinnedConnectionAfterRequestCancellation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	mock.ExpectQuery(`SELECT GET_LOCK`).WithArgs("private-intent").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	d := &Database{db: db}
	ctx, cancel := context.WithCancel(context.Background())
	release, locked, err := d.TryPublicAgentIntentLock(ctx, "private-intent")
	if err != nil || !locked {
		t.Fatalf("lock: %v %v", locked, err)
	}
	cancel()
	mock.ExpectQuery(`SELECT RELEASE_LOCK`).WithArgs("private-intent").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	release()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
