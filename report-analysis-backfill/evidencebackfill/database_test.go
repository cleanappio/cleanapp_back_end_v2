package evidencebackfill

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestBackfillIncludesNonEnglishReportsAndGroupsTranslationsBySequence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// No en-only predicate: a brand report with only und/ja/etc. analyses remains
	// eligible. GROUP BY seq means translations do not inflate label counts.
	mock.ExpectQuery(`SELECT ra.seq, MAX\(ra.brand_name\).*brand_name IN \(\?,\?\).*GROUP BY ra.seq ORDER BY ra.seq ASC LIMIT`).WithArgs(0, "grok", "claude", 50).WillReturnRows(sqlmock.NewRows([]string{"seq", "brand"}).AddRow(4, "grok").AddRow(5, "claude"))
	got, err := (DatabaseStore{DB: db}).Candidates(context.Background(), Options{Brands: []string{"grok", "claude"}, Limit: 50})
	if err != nil || len(got) != 2 || got[1].Seq != 5 {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
