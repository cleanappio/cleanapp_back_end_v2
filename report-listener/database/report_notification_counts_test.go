package database

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNotificationCountsUseOneSelectedPageAggregateAndKnownZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query, _, err := reportNotificationCountQuery([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1, 2, 3, 1, 2, 3).
		WillReturnRows(sqlmock.NewRows([]string{"seq", "email_count"}).AddRow(1, 4).AddRow(2, 2))
	counts, err := (&Database{db: db}).ReportNotificationCounts(context.Background(), []int{3, 2, 1, 1, 0, -1})
	if err != nil {
		t.Fatal(err)
	}
	for seq, want := range map[int]int{1: 4, 2: 2, 3: 0} {
		got := counts[seq]
		if got.EmailCount == nil || *got.EmailCount != want || (got.State == "sent") != (want > 0) {
			t.Fatalf("seq=%d count=%+v want=%d", seq, got, want)
		}
	}
	if counts[3].State != "none_recorded" || len(counts) != 3 {
		t.Fatalf("known zero or selected scope changed: %+v", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationCountQueryReturnsOnlyCountAndKeepsSharedNullSafeDedup(t *testing.T) {
	query, args, err := reportNotificationCountQuery([]int{7, 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 4 || strings.Count(query, "seq IN (?,?)") != 2 || !strings.Contains(query, reportNotificationLegacyUnrecordedSQL) {
		t.Fatalf("unbounded or divergent notification query: %s", query)
	}
	if !strings.HasPrefix(query, "SELECT notifications.seq, COUNT(*) AS email_count") ||
		!strings.Contains(query, "REGEXP_LIKE(recipient_email, '[^[:space:]]')") {
		t.Fatalf("count/blank projection changed: %s", query)
	}
	for _, forbidden := range []string{"SELECT recipient_email", "SELECT old.recipient_email", "provider", "sent_reports_emails", "case_reports", "GROUP BY recipient_email"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("count query leaked/broadened data through %q: %s", forbidden, query)
		}
	}
}

func TestNotificationCountErrorsAreUnknownAndBounded(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	database := &Database{db: db}
	counts, err := database.ReportNotificationCounts(context.Background(), nil)
	if err != nil || len(counts) != 0 {
		t.Fatalf("empty page must not query ledger: %+v %v", counts, err)
	}
	tooMany := make([]int, maxPublicNotificationCountReports+1)
	for index := range tooMany {
		tooMany[index] = index + 1
	}
	if counts, err := database.ReportNotificationCounts(context.Background(), tooMany); err == nil || counts != nil {
		t.Fatal("unbounded report set reached database")
	}
	query, _, _ := reportNotificationCountQuery([]int{1})
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1, 1).WillReturnError(errors.New("ledger unavailable"))
	if counts, err := database.ReportNotificationCounts(context.Background(), []int{1}); err == nil || counts != nil {
		t.Fatal("failed count query must not fabricate zeros")
	}
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(1, 1).
		WillReturnRows(sqlmock.NewRows([]string{"seq", "email_count"}).AddRow(1, 4).AddRow(2, 7))
	if counts, err := database.ReportNotificationCounts(context.Background(), []int{1}); err == nil || counts != nil {
		t.Fatal("unexpected report row escaped selected scope")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
