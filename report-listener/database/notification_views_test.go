package database

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRecordNotificationView(t *testing.T) {
	sent := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	target := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"public_id", "recipient_email", "sent_at", "classification"}).AddRow("rpt_abc", " Support@X.ai ", sent, "digital")
	}
	lookup := regexp.QuoteMeta(notificationViewTargetSQL)
	insert := regexp.QuoteMeta(notificationViewInsertSQL)
	const id = "0123456789abcdef"

	for name, tc := range map[string]struct {
		human       bool
		at          time.Time
		found       bool
		insertErr   error
		wantInsert  bool
		wantTarget  bool
		wantErr     bool
		lookupFails bool
	}{
		"person records one view":            {human: true, at: sent.Add(time.Minute), found: true, wantInsert: true, wantTarget: true},
		"scanner user agent only redirects":  {human: false, at: sent.Add(time.Minute), found: true, wantTarget: true},
		"instant fetch after send is a scan": {human: true, at: sent.Add(time.Second), found: true, wantTarget: true},
		"unknown or private report":          {human: true, at: sent.Add(time.Minute)},
		"failed write still redirects":       {human: true, at: sent.Add(time.Minute), found: true, wantInsert: true, insertErr: errors.New("db down"), wantTarget: true, wantErr: true},
		"failed lookup reveals nothing":      {human: true, at: sent.Add(time.Minute), lookupFails: true, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			q := mock.ExpectQuery(lookup).WithArgs(id, int64(42))
			switch {
			case tc.lookupFails:
				q.WillReturnError(errors.New("db down"))
			case tc.found:
				q.WillReturnRows(target())
			default:
				q.WillReturnRows(sqlmock.NewRows([]string{"public_id", "recipient_email", "sent_at", "classification"}))
			}
			if tc.wantInsert {
				// Recipient is normalised; the report is keyed by public ID.
				e := mock.ExpectExec(insert).WithArgs("rpt_abc", "support@x.ai", id, id, "rpt_abc", id)
				if tc.insertErr != nil {
					e.WillReturnError(tc.insertErr)
				} else {
					e.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			got, err := (&Database{db: db}).RecordNotificationView(context.Background(), id, 42, tc.human, tc.at)
			if (err != nil) != tc.wantErr || (got != nil) != tc.wantTarget {
				t.Fatalf("target=%v err=%v", got, err)
			}
			if got != nil && (got.PublicID != "rpt_abc" || !got.Digital) {
				t.Fatalf("wrong target: %#v", got)
			}
			// sqlmock fails on any statement that was not expected, so the
			// no-insert cases prove nothing was written.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
