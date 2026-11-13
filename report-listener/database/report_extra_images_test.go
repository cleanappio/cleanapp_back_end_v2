package database

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestExtraImageIsServedOnlyUnderTheFirstPhotosRules(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &Database{db: db}

	// The query must carry both public rules; QuoteMeta makes that literal.
	query := regexp.QuoteMeta("JOIN report_extra_images x ON x.seq = r.seq AND x.position = ?") + `[\s\S]*` +
		regexp.QuoteMeta(publicReportCurrentVisibilitySQL) + `[\s\S]*` + regexp.QuoteMeta(publicReportMediaPrivacySQL)
	mock.ExpectQuery(query).WithArgs(2, "rpt_abc").WillReturnRows(sqlmock.NewRows([]string{"image"}).AddRow([]byte("photo-3")))
	got, err := d.GetExtraImageByPublicID(context.Background(), "rpt_abc", 2)
	if err != nil || string(got) != "photo-3" {
		t.Fatalf("got %q, %v", got, err)
	}

	// No row (no such photo, private report, or media withheld) is "nothing", not an error.
	mock.ExpectQuery(query).WithArgs(1, "rpt_private").WillReturnRows(sqlmock.NewRows([]string{"image"}))
	if got, err := d.GetExtraImageByPublicID(context.Background(), "rpt_private", 1); err != nil || got != nil {
		t.Fatalf("got %q, %v", got, err)
	}

	// Out-of-range positions never reach the database.
	for _, position := range []int{0, -1, 4} {
		if got, err := d.GetExtraImageByPublicID(context.Background(), "rpt_abc", position); err != nil || got != nil {
			t.Fatalf("position %d: got %q, %v", position, got, err)
		}
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM report_extra_images")).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(3))
	if n, err := d.ReportExtraImageCount(context.Background(), 7); err != nil || n != 3 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
