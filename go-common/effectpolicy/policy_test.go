package effectpolicy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSuppressionPreservesOrdinaryReportsAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name                                              string
		version                                           int
		blocked, noRow, readError, wantBlocked, wantError bool
	}{
		{name: "ordinary report", noRow: true},
		{name: "explicit permission", version: 1},
		{name: "historical suppression", version: 1, blocked: true, wantBlocked: true},
		{name: "future policy cannot authorize", version: 2, wantBlocked: true, wantError: true},
		{name: "database unavailable", readError: true, wantBlocked: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			query := mock.ExpectQuery(`SELECT policy_version, suppress_email FROM report_side_effect_policy WHERE report_seq=\?`).WithArgs(int64(42))
			if tc.readError {
				query.WillReturnError(errors.New("policy table unavailable"))
			} else {
				rows := sqlmock.NewRows([]string{"policy_version", "suppress_email"})
				if !tc.noRow {
					rows.AddRow(tc.version, tc.blocked)
				}
				query.WillReturnRows(rows)
			}
			blocked, err := Suppressed(context.Background(), db, 42, Email)
			if blocked != tc.wantBlocked || (err != nil) != tc.wantError {
				t.Fatalf("blocked=%v error=%v", blocked, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidPolicyIdentifiersCannotWidenPermission(t *testing.T) {
	for _, args := range [][3]string{{"r; DROP TABLE reports", "seq", "email"}, {"r", "seq OR TRUE", "email"}, {"r", "seq", "unknown"}} {
		if got := AllowSQL(args[0], args[1], Effect(args[2])); got != "FALSE" {
			t.Fatalf("unsafe predicate: %s", got)
		}
	}
	if got := AllowSQL("r", "seq", Email); !strings.Contains(got, "effect_policy.report_seq=r.seq") || !strings.Contains(got, "suppress_email") || !strings.Contains(got, "policy_version") {
		t.Fatalf("missing guard: %s", got)
	}
}

func TestCasePolicyLookupFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`(?s)SELECT EXISTS.*case_reports.*anchor_report_seq.*suppress_email`).WithArgs("case-1", "case-1").WillReturnError(errors.New("lookup failed"))
	blocked, err := CaseSuppressed(context.Background(), db, "case-1", Email)
	if !blocked || err == nil {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
