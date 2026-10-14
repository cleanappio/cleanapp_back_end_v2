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
	mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(`{"digital_context":{"metadata":{"original_text":"My original reproduction"}},"privacy":{"requires_redaction":true}}`, "machine", "wire"))
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
			mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw`).WithArgs(6).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(`{"digital_context":{"metadata":{"original_text":"Partial","original_text_truncated":true}}}`, "machine", "wire"))
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
	mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw`).WillReturnError(errors.New("Table doesn't exist"))
	got, err := LoadOriginal(context.Background(), db, 7)
	if err != nil || got.Text != "" || got.SourceType != "unknown" {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeHumanAndSharedIngestionUsesActualOriginalBodies(t *testing.T) {
	for _, tt := range []struct{ name, sourceID, sourceType, actor, channel, wire, shared, want, kind string }{
		{"mobile annotation", "human_mobile_abc123", "sensor", "human", "mobile", `{"problem_type":"citizen_submission","description":"I found the ramp blocked by bins. Original annotation continues beyond the truncated public description.","title":"I found the ramp"}`, "", "I found the ramp blocked by bins. Original annotation continues beyond the truncated public description.", "human_submission"},
		{"web annotation", "human_web_abc123", "sensor", "human", "web", `{"problem_type":"citizen_submission","description":"I cannot enter the building."}`, "", "I cannot enter the building.", "human_submission"},
		{"share dedicated body", "human:share:abc123", "web", "human", "mobile", `{"problem_type":"shared_content_report","description":"Shared URL: https://example.com\nShared text: shorter\nSource app: App","digital_context":{"shared_text":"shorter"}}`, "I cannot submit the form. Full shared original.", "I cannot submit the form. Full shared original.", "digital_share"},
		{"share retained body before metadata hook", "human:share:abc123", "web", "human", "mobile", `{"problem_type":"shared_content_report","description":"Shared URL: https://example.com","digital_context":{"shared_text":"I hit an error."}}`, "", "I hit an error.", "digital_share"},
		{"machine cannot claim human", "human_mobile_forged", "sensor", "machine", "wire", `{"problem_type":"citizen_submission","description":"I saw a failure (generated)","agent":{"operator_type":"human"}}`, "", "", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("Truncated/synthetic display text", nil))
			mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
			mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow(tt.sourceID, tt.sourceType))
			mock.ExpectQuery(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw`).WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(tt.wire, tt.actor, tt.channel))
			if tt.kind == "digital_share" {
				rows := sqlmock.NewRows([]string{"shared_text"})
				if tt.shared != "" {
					rows.AddRow(tt.shared)
				}
				mock.ExpectQuery(`SELECT shared_text FROM digital_share_reports`).WithArgs(9).WillReturnRows(rows)
			}
			got, err := LoadOriginal(context.Background(), db, 9)
			if err != nil || got.Text != tt.want || got.SourceType != tt.kind {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
