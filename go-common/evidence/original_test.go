package evidence

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestLoadOriginalUsesRetainedSnapshotAndPrivacy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("generated summary", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}).AddRow("github_issue", "123"))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}))
	mock.ExpectQuery(`SELECT report_json FROM wire_submissions_raw`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"report_json"}).AddRow(`{"digital_context":{"metadata":{"original_text":"My original reproduction"}},"privacy":{"requires_redaction":true}}`))
	got, err := LoadOriginal(context.Background(), db, 5)
	if err != nil || got.Text != "My original reproduction" || !got.RequiresRedaction {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOriginalTruncatedSnapshotFallsBackToActualIndexerSchema(t *testing.T) {
	for _, tt := range []struct{ source, id, query, original string }{
		{"github_issue", "123", `SELECT CONCAT(COALESCE(title, ''), '\n\n', COALESCE(body, '')) FROM indexer_github_issue WHERE issue_id = ?`, "Title\n\nFull original body"},
		{"twitter", "999", `SELECT text FROM indexer_twitter_tweet WHERE tweet_id = ?`, "Full original tweet"},
		{"bluesky", "at://did:plc:abc/app.bsky.feed.post/xyz", `SELECT text FROM indexer_bluesky_post WHERE uri = ?`, "Full original Bluesky post"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(6).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("generated summary", nil))
			mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(6).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}).AddRow(tt.source, tt.id))
			mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(6).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}))
			mock.ExpectQuery(`SELECT report_json FROM wire_submissions_raw`).WithArgs(6).WillReturnRows(sqlmock.NewRows([]string{"report_json"}).AddRow(`{"digital_context":{"metadata":{"original_text":"Partial","original_text_truncated":true}}}`))
			mock.ExpectQuery(regexp.QuoteMeta(tt.query)).WithArgs(tt.id).WillReturnRows(sqlmock.NewRows([]string{"original"}).AddRow(tt.original))
			got, err := LoadOriginal(context.Background(), db, 6)
			if err != nil || got.Text != tt.original {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMissingIntakeTablesNeverTreatGeneratedDescriptionAsDirect(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("I hit a bug (generated)", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WillReturnError(errors.New("Table doesn't exist"))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WillReturnError(errors.New("Table doesn't exist"))
	mock.ExpectQuery(`SELECT report_json FROM wire_submissions_raw`).WillReturnError(errors.New("Table doesn't exist"))
	got, err := LoadOriginal(context.Background(), db, 7)
	if err != nil || got.Text != "" || got.SourceType != "unknown" {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
