package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCompanyNotificationLockCanonicalAliasRunsAndReleases(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT GET_LOCK\(\?,0\)`).WithArgs("cleanapp:company-notification:xai").WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(1))
	mock.ExpectQuery(`SELECT RELEASE_LOCK\(\?\)`).WithArgs("cleanapp:company-notification:xai").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
	calls := 0
	ran, err := (&EmailService{db: db}).withCompanyNotificationLock(context.Background(), "  GROK  ", func() error { calls++; return nil })
	if err != nil || !ran || calls != 1 {
		t.Fatalf("canonical company work did not run once: ran=%v calls=%d err=%v", ran, calls, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyNotificationLockBusyAndErrorsFailClosed(t *testing.T) {
	for _, scenario := range []string{"busy", "null", "unexpected", "query_error", "scan_error"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			query := mock.ExpectQuery(`SELECT GET_LOCK\(\?,0\)`).WithArgs("cleanapp:company-notification:anthropic")
			switch scenario {
			case "busy":
				query.WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(0))
			case "null":
				query.WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(nil))
			case "unexpected":
				query.WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(2))
			case "query_error":
				query.WillReturnError(errors.New("database unavailable"))
			case "scan_error":
				query.WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow("invalid response"))
			}
			if scenario != "busy" {
				mock.ExpectClose()
			}
			calls := 0
			ran, err := (&EmailService{db: db}).withCompanyNotificationLock(context.Background(), "claude-code", func() error { calls++; return nil })
			if ran || calls != 0 || (scenario == "busy" && err != nil) || (scenario != "busy" && err == nil) {
				t.Fatalf("unowned lock permitted work: ran=%v calls=%d err=%v", ran, calls, err)
			}
			if scenario != "busy" && db.Stats().OpenConnections != 0 {
				t.Fatal("possibly locked acquisition session returned to the pool")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompanyNotificationLockReleasesAfterWorkErrorAndCanceledContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT GET_LOCK\(\?,0\)`).WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(1))
	mock.ExpectQuery(`SELECT RELEASE_LOCK\(\?\)`).WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
	ctx, cancel := context.WithCancel(context.Background())
	workErr := errors.New("send rejected")
	ran, err := (&EmailService{db: db}).withCompanyNotificationLock(ctx, "openai", func() error { cancel(); return workErr })
	if !ran || !errors.Is(err, workErr) {
		t.Fatalf("work error was lost: ran=%v err=%v", ran, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyNotificationLockDiscardsSessionAfterReleaseFailure(t *testing.T) {
	for _, scenario := range []string{"error", "null", "not_owned"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			// The driver close expectation is meaningful: returning the session
			// to the idle pool could leave its named lock permanently held.
			mock.ExpectQuery(`SELECT GET_LOCK\(\?,0\)`).WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(1))
			query := mock.ExpectQuery(`SELECT RELEASE_LOCK\(\?\)`)
			switch scenario {
			case "error":
				query.WillReturnError(errors.New("release unavailable"))
			case "null":
				query.WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(nil))
			case "not_owned":
				query.WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(0))
			}
			mock.ExpectClose()
			ran, err := (&EmailService{db: db}).withCompanyNotificationLock(context.Background(), "grok", func() error { return nil })
			if !ran || err == nil || !strings.Contains(err.Error(), "release") || db.Stats().OpenConnections != 0 {
				t.Fatalf("possibly locked session returned to pool: ran=%v open=%d err=%v", ran, db.Stats().OpenConnections, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompanyNotificationLockRejectsUnknownCompanyWithoutDatabase(t *testing.T) {
	if ran, err := (&EmailService{}).withCompanyNotificationLock(context.Background(), "X", func() error { t.Fatal("X is not xAI"); return nil }); ran || err == nil {
		t.Fatalf("unknown company accepted: ran=%v err=%v", ran, err)
	}
}
