package evidence

import (
	"context"
	"errors"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"strings"
	"testing"
)

func expectLatestWire(mock sqlmock.Sqlmock, seq int, payload, actor, channel string) {
	submissionID := fmt.Sprintf("retained-wire-%d", seq)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT submission_id FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`)).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow(submissionID))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = ? AND report_seq = ?`)).WithArgs(submissionID, seq).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(payload, actor, channel))
}

func expectOriginalIntake(mock sqlmock.Sqlmock, seq int, source, id, payload string) {
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("generated summary", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}).AddRow(source, id))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(seq).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}))
	expectLatestWire(mock, seq, payload, "machine", "wire")
}

func TestRetainedOriginalPreservesSourceContextWithoutIndexerFetch(t *testing.T) {
	for _, tt := range []struct{ name, source, payload, author, relation string }{
		{"explicit Twitter context", "twitter", `{"digital_context":{"metadata":{"original_text":"My original report","source_author":"grok","source_relation":" REPLY "}}}`, "grok", "reply"},
		{"existing Twitter alias", "twitter", `{"digital_context":{"metadata":{"original_text":"My original report","author_username":"reporter","source_relation":"quote"}}}`, "reporter", "quote"},
		{"existing Bluesky alias", "bluesky", `{"digital_context":{"metadata":{"original_text":"My original report","author_handle":"reporter.bsky.social","source_relation":"original"}}}`, "reporter.bsky.social", "original"},
		{"digital context Bluesky alias", "bluesky", `{"digital_context":{"author_handle":"reporter.bsky.social","metadata":{"original_text":"My original report","source_relation":"reply"}}}`, "reporter.bsky.social", "reply"},
		{"explicit unknown relation", "twitter", `{"digital_context":{"metadata":{"original_text":"My original report","source_author":"reporter","source_relation":"this is first person"}}}`, "reporter", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expectOriginalIntake(mock, 15, tt.source, "source-id", tt.payload)
			got, err := LoadOriginal(context.Background(), db, 15)
			if err != nil || got.Text != "My original report" || got.SourceAuthor != tt.author || got.SourceRelation != tt.relation {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRetainedSnapshotLoadsContextWithoutReplacingImmutableText(t *testing.T) {
	for _, tt := range []struct {
		source, query, payload, author, relation string
		columns                                  []string
		values                                   []any
	}{
		{"twitter", `SELECT username, relation FROM indexer_twitter_tweet WHERE tweet_id = ?`, `{"digital_context":{"metadata":{"original_text":"Immutable snapshot"}}}`, "grok", "reply", []string{"username", "relation"}, []any{"grok", "reply"}},
		{"bluesky", `SELECT author_handle, CAST(raw AS CHAR) FROM indexer_bluesky_post WHERE uri = ?`, `{"digital_context":{"metadata":{"original_text":"Immutable snapshot","author_handle":"retained.bsky.social"}}}`, "retained.bsky.social", "quote", []string{"author_handle", "raw"}, []any{"mutable.bsky.social", `{"$type":"app.bsky.feed.post","embed":{"$type":"app.bsky.embed.record","record":{"uri":"at://did:plc:abc/app.bsky.feed.post/post"}}}`}},
	} {
		t.Run(tt.source, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expectOriginalIntake(mock, 16, tt.source, "source-id", tt.payload)
			result := sqlmock.NewRows(tt.columns)
			result.AddRow(tt.values[0], tt.values[1])
			mock.ExpectQuery(regexp.QuoteMeta(tt.query)).WithArgs("source-id").WillReturnRows(result)
			got, err := LoadOriginal(context.Background(), db, 16)
			if err != nil || got.Text != "Immutable snapshot" || got.SourceAuthor != tt.author || got.SourceRelation != tt.relation {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOriginalSourceContextBoundsAndBlueskyRelations(t *testing.T) {
	if got := normalizeSourceAuthor("  a\x00b\n" + strings.Repeat("x", 300)); strings.ContainsAny(got, "\x00\n") || len([]rune(got)) > 253 {
		t.Fatalf("unsafe source author: %q", got)
	}
	for _, author := range []string{"did:plc:private-account-id", "at://did:plc:account/app.bsky.feed.post/post", "contact@example.com", "https://example.com", "arbitrary source instructions"} {
		if got := normalizeSourceAuthor(author); got != "" {
			t.Fatalf("non-handle leaked as source author: %q", got)
		}
	}
	for _, tt := range []struct{ raw, want string }{
		{`{"$type":"app.bsky.feed.post","text":"Observed issue"}`, "original"},
		{`{"record":{"$type":"app.bsky.feed.post","reply":{"parent":{"uri":"at://did:plc:abc/app.bsky.feed.post/post"}}}}`, "reply"},
		{`{"$type":"app.bsky.feed.post","embed":{"$type":"app.bsky.embed.recordWithMedia","record":{"record":{"uri":"at://did:plc:abc/app.bsky.feed.post/post"}}}}`, "quote"},
		{`{"record":{"text":"Old partial record"},"embed":{"$type":"app.bsky.embed.record#view","record":{"uri":"at://did:plc:abc/app.bsky.feed.post/post"}}}`, "quote"},
		{`{"record":{"text":"Old partial record"}}`, "unknown"},
		{`{"$type":"app.bsky.feed.post","reply":"untrusted text claiming a reply"}`, "unknown"},
		{`{"$type":"app.bsky.feed.post","embed":{"$type":"app.bsky.embed.record"}}`, "unknown"},
		{`not JSON`, "unknown"},
	} {
		if got := blueskySourceRelation([]byte(tt.raw)); got != tt.want {
			t.Errorf("raw %s: got %s want %s", tt.raw, got, tt.want)
		}
	}
}

func TestLoadOriginalUsesRetainedSnapshotAndPrivacy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("generated summary", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}).AddRow("github_issue", "123"))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(5).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}))
	expectLatestWire(mock, 5, `{"digital_context":{"metadata":{"original_text":"My original reproduction"}},"privacy":{"requires_redaction":true}}`, "machine", "wire")
	got, err := LoadOriginal(context.Background(), db, 5)
	if err != nil || got.Text != "My original reproduction" || !got.RequiresRedaction {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLargeWireOriginalUsesSelectedSubmissionAndPreservesPrivacy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("Generated display summary", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("human_mobile_fixture", "sensor"))
	// The selected key is unrelated to the report sequence and must be used to
	// hydrate the exact submission, retaining its authenticated channel/privacy.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT submission_id FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`)).WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("newest-selected-source"))
	payload := `{"problem_type":"citizen_submission","description":"After clicking Save, the application closes.","privacy":{"requires_redaction":true},"image_base64":"` + strings.Repeat("A", 512000) + `"}`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = ? AND report_seq = ?`)).WithArgs("newest-selected-source", 20).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}).AddRow(payload, "human", "mobile"))
	got, err := LoadOriginal(context.Background(), db, 20)
	if err != nil || got.Text != "After clicking Save, the application closes." || got.SourceType != "human_submission" || !got.RequiresRedaction || got.SourceAuthor != "" {
		t.Fatalf("original source/provenance/privacy changed: source=%s redacted=%t err=%v", got.SourceType, got.RequiresRedaction, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWireLookupErrorsDoNotUseGeneratedDescription(t *testing.T) {
	for _, stage := range []string{"select key", "hydrate original"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(21).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("I hit a bug (generated)", nil))
			mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(21).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
			mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(21).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("human_mobile_fixture", "sensor"))
			keyQuery := mock.ExpectQuery(regexp.QuoteMeta(`SELECT submission_id FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`)).WithArgs(21)
			if stage == "select key" {
				keyQuery.WillReturnError(errors.New("temporary source database failure"))
			} else {
				keyQuery.WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("selected-source"))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = ? AND report_seq = ?`)).WithArgs("selected-source", 21).WillReturnError(errors.New("temporary source database failure"))
			}
			got, err := LoadOriginal(context.Background(), db, 21)
			if err == nil || got.Text != "" {
				t.Fatal("source resolution failure must remain an error, without generated text")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWireSubmissionUnlinkedDuringLookupCannotAuthorizeHumanFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT description, image FROM reports`).WithArgs(22).WillReturnRows(sqlmock.NewRows([]string{"description", "image"}).AddRow("I hit a bug (generated)", nil))
	mock.ExpectQuery(`SELECT source, external_id FROM external_ingest_index`).WithArgs(22).WillReturnRows(sqlmock.NewRows([]string{"source", "external_id"}))
	mock.ExpectQuery(`SELECT source_id, source_type FROM report_raw`).WithArgs(22).WillReturnRows(sqlmock.NewRows([]string{"source_id", "source_type"}).AddRow("human_mobile_fixture", "sensor"))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT submission_id FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`)).WithArgs(22).WillReturnRows(sqlmock.NewRows([]string{"submission_id"}).AddRow("unlinked-source"))
	// The original key no longer belongs to this report when it is hydrated.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE submission_id = ? AND report_seq = ?`)).WithArgs("unlinked-source", 22).WillReturnRows(sqlmock.NewRows([]string{"report_json", "actor_kind", "channel"}))
	got, err := LoadOriginal(context.Background(), db, 22)
	if err != nil || got.Text != "" || got.SourceType != "unknown" {
		t.Fatal("unlinked Wire source authorized generated human content")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOriginalTruncatedSnapshotFallsBackToActualIndexerSchema(t *testing.T) {
	for _, tt := range []struct{ source, id, query, original, author, relation string }{
		{"github_issue", "123", `SELECT CONCAT(COALESCE(title, ''), '\n\n', COALESCE(body, '')) FROM indexer_github_issue WHERE issue_id = ?`, "Title\n\nFull original body", "", "unknown"},
		{"twitter", "999", `SELECT text, username, relation FROM indexer_twitter_tweet WHERE tweet_id = ?`, "Full original tweet", "reporter", "retweet"},
		{"bluesky", "at://did:plc:abc/app.bsky.feed.post/xyz", `SELECT text, author_handle, CAST(raw AS CHAR) FROM indexer_bluesky_post WHERE uri = ?`, "Full original Bluesky post", "reporter.bsky.social", "original"},
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
			expectLatestWire(mock, 6, `{"digital_context":{"metadata":{"original_text":"Partial","original_text_truncated":true}}}`, "machine", "wire")
			result := sqlmock.NewRows([]string{"original"}).AddRow(tt.original)
			if tt.source == "twitter" {
				result = sqlmock.NewRows([]string{"text", "username", "relation"}).AddRow(tt.original, tt.author, tt.relation)
			}
			if tt.source == "bluesky" {
				result = sqlmock.NewRows([]string{"text", "author_handle", "raw"}).AddRow(tt.original, tt.author, `{"$type":"app.bsky.feed.post","text":"Full original Bluesky post"}`)
			}
			mock.ExpectQuery(regexp.QuoteMeta(tt.query)).WithArgs(tt.id).WillReturnRows(result)
			got, err := LoadOriginal(context.Background(), db, 6)
			if err != nil || got.Text != tt.original || got.SourceAuthor != tt.author || got.SourceRelation != tt.relation {
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
	mock.ExpectQuery(`SELECT submission_id FROM wire_submissions_raw`).WillReturnError(errors.New("Table doesn't exist"))
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
		{"native metadata cannot infer source author", "human_web_abc123", "sensor", "human", "web", `{"problem_type":"citizen_submission","description":"I cannot enter the building.","digital_context":{"metadata":{"source_author":"grok","source_relation":"quote"}}}`, "", "I cannot enter the building.", "human_submission"},
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
			expectLatestWire(mock, 9, tt.wire, tt.actor, tt.channel)
			if tt.kind == "digital_share" {
				rows := sqlmock.NewRows([]string{"shared_text"})
				if tt.shared != "" {
					rows.AddRow(tt.shared)
				}
				mock.ExpectQuery(`SELECT shared_text FROM digital_share_reports`).WithArgs(9).WillReturnRows(rows)
			}
			got, err := LoadOriginal(context.Background(), db, 9)
			if err != nil || got.Text != tt.want || got.SourceType != tt.kind || got.SourceAuthor != "" || got.SourceRelation != "unknown" {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOlderIndexerSchemaPreservesOriginalAndAvailableContext(t *testing.T) {
	for _, tt := range []struct {
		name, source, payload string
		fullText              bool
	}{
		{"Twitter relation missing", "twitter", `{}`, true},
		{"old Twitter author and relation missing", "twitter", `{}`, true},
		{"Bluesky raw missing", "bluesky", `{}`, true},
		{"retained Twitter context missing", "twitter", `{"digital_context":{"metadata":{"original_text":"Immutable snapshot"}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expectOriginalIntake(mock, 18, tt.source, "source-id", tt.payload)
			var wantText, wantAuthor string
			if tt.source == "bluesky" {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT text, author_handle, CAST(raw AS CHAR) FROM indexer_bluesky_post WHERE uri = ?`)).WithArgs("source-id").WillReturnError(errors.New("Unknown column 'raw'"))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT text, author_handle FROM indexer_bluesky_post WHERE uri = ?`)).WithArgs("source-id").WillReturnRows(sqlmock.NewRows([]string{"text", "author_handle"}).AddRow("Original post", "reporter.bsky.social"))
				wantText, wantAuthor = "Original post", "reporter.bsky.social"
			} else if tt.fullText {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT text, username, relation FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnError(errors.New("Unknown column 'relation'"))
				if tt.name == "old Twitter author and relation missing" {
					mock.ExpectQuery(regexp.QuoteMeta(`SELECT text, username FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnError(errors.New("Unknown column 'username'"))
					mock.ExpectQuery(regexp.QuoteMeta(`SELECT text FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnRows(sqlmock.NewRows([]string{"text"}).AddRow("Original post"))
					wantText = "Original post"
				} else {
					mock.ExpectQuery(regexp.QuoteMeta(`SELECT text, username FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnRows(sqlmock.NewRows([]string{"text", "username"}).AddRow("Original post", "reporter"))
					wantText, wantAuthor = "Original post", "reporter"
				}
			} else {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT username, relation FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnError(errors.New("Unknown column 'relation'"))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT username FROM indexer_twitter_tweet WHERE tweet_id = ?`)).WithArgs("source-id").WillReturnError(errors.New("Unknown column 'username'"))
				wantText = "Immutable snapshot"
			}
			got, err := LoadOriginal(context.Background(), db, 18)
			if err != nil || got.Text != wantText || got.SourceAuthor != wantAuthor || got.SourceRelation != "unknown" {
				t.Fatalf("got %#v err=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
