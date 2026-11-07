package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cleanapp-common/effectpolicy"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestHistoricalReporterRewardDoesNotWriteBalances(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBegin()
		q := mock.ExpectQuery(`SELECT policy_version, suppress_rewards FROM report_side_effect_policy`).WithArgs(int64(42))
		if unavailable {
			q.WillReturnError(errors.New("policy unavailable"))
		} else {
			q.WillReturnRows(sqlmock.NewRows([]string{"policy_version", "suppress_rewards"}).AddRow(1, true))
		}
		mock.ExpectRollback()
		err = (&Database{db: db}).IncrementReporterDailyKITNs(context.Background(), "legacy-user", 42)
		if (err != nil) != unavailable {
			t.Fatalf("unavailable=%v error=%v", unavailable, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestOrdinaryReporterRewardRetainsExistingCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT policy_version, suppress_rewards FROM report_side_effect_policy`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"policy_version", "suppress_rewards"}))
	mock.ExpectExec(`UPDATE users SET kitns_daily = kitns_daily \+ 1`).WithArgs("ordinary-user").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE users_shadow SET kitns_daily = kitns_daily \+ 1`).WithArgs("ordinary-user").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := (&Database{db: db}).IncrementReporterDailyKITNs(context.Background(), "ordinary-user", 42); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalLiveBroadcastSelectorIsSuppressed(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		if !strings.Contains(actual, effectpolicy.AllowSQL("r", "seq", effectpolicy.Notifications)) || !strings.Contains(actual, "r.seq > ?") || !strings.Contains(actual, PublicVisibilityWhereSQL) {
			return fmt.Errorf("live selector lacks scoped historical policy")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("live selector").WithArgs(41).WillReturnRows(sqlmock.NewRows([]string{"seq"}))
	reports, err := (&Database{db: db}).GetReportsSince(context.Background(), 41)
	if err != nil || len(reports) != 0 {
		t.Fatalf("reports=%v err=%v", reports, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalPushSelectorIsSuppressed(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		if !strings.Contains(actual, effectpolicy.AllowSQL("rps", "report_seq", effectpolicy.Notifications)) {
			return fmt.Errorf("push selector lacks historical policy")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("push selector").WithArgs(42).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	devices, err := (&Database{db: db}).GetReportPushDevices(context.Background(), 42)
	if err != nil || len(devices) != 0 {
		t.Fatalf("devices=%v err=%v", devices, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
