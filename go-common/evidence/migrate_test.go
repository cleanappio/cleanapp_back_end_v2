package evidence

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
)

func TestMigrationCreatesOnlyPerReportStorageAndCanRunTwice(t *testing.T) {
	var statements []string
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		statements = append(statements, actual)
		upper := strings.ToUpper(actual)
		if !strings.HasPrefix(strings.TrimSpace(upper), "CREATE TABLE IF NOT EXISTS REPORT_EVIDENCE") || strings.Contains(upper, "ALTER ") || strings.Contains(upper, "REPORT_ANALYSIS") || strings.Contains(upper, "MIGRATION_HISTORY") {
			return errors.New("migration touched an existing schema or ledger")
		}
		for _, definition := range []string{"SEQ INT", "PRIMARY KEY", "EVIDENCE_VERSION INT", "DEFAULT 0", "ENGINE=INNODB", "UTF8MB4_UNICODE_CI"} {
			if !strings.Contains(upper, definition) {
				return errors.New("missing per-report storage contract")
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		mock.ExpectExec("create evidence table").WillReturnResult(sqlmock.NewResult(0, 0))
		if err := Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if len(statements) != 2 {
		t.Fatalf("expected one safe CREATE per invocation: %#v", statements)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationPropagatesCreateFailureWithoutExistingTableWork(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("create denied")
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS report_evidence`).WillReturnError(failure)
	if err := Migrate(context.Background(), db); !errors.Is(err, failure) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
