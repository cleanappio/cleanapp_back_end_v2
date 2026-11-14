package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAgentOriginalHydratesActualMCPAndPublicShapesWithoutInventingFacts(t *testing.T) {
	longDescription := strings.Repeat("The checkout failed after I chose delivery. ", 20)
	for _, tc := range []struct {
		name, channel string
		report        map[string]any
		wantText      string
	}{
		{"MCP digital", "wire", map[string]any{"title": "Checkout fails", "description": longDescription,
			"digital_context": map[string]any{"product_or_site": "Fixture shop", "url": "https://shop.example/checkout"}},
			"Checkout fails\n\n" + longDescription},
		{"public digital", "public_agent", map[string]any{"title": longDescription[:255], "description": longDescription,
			"digital_context": map[string]any{"url": "https://shop.example/checkout"}}, longDescription},
		{"MCP physical", "wire", map[string]any{"title": "Blocked door", "description": "I found the entrance blocked.",
			"location": map[string]any{"lat": 47.37, "lng": 8.55, "address_text": "Fixture entrance"}},
			"Blocked door\n\nI found the entrance blocked."},
		{"public unknown context", "public_agent", map[string]any{"description": "The checkout button failed."}, "The checkout button failed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.report)
			got, err := decodeAgentOutcomeOriginal(raw, "retained-source", tc.channel)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(got.Original.Text) != strings.TrimSpace(tc.wantText) || string(got.Context) != string(raw) {
				t.Fatal("lost full retained observation or changed supplied context")
			}
			if got.Original.SourceType != "machine_submission" || got.Original.SourceRelation != "unknown" ||
				got.Original.SourceID != "retained-source" || got.Original.DirectSubmission() {
				t.Fatal("machine provenance was promoted to verified human authorship")
			}
			var projection map[string]any
			_ = json.Unmarshal(got.Context, &projection)
			if _, exists := projection["observed_at"]; exists {
				t.Fatal("invented observation time")
			}
			if _, supplied := tc.report["location"]; !supplied && projection["location"] != nil {
				t.Fatal("invented location for a missing-context report")
			}
		})
	}
}

func TestAgentOriginalPreservesExplicitSourceSnapshotAndPrivacyRestriction(t *testing.T) {
	raw := []byte(`{"title":"Generated wrapper","description":"Generated summary must not replace the source.","digital_context":{"metadata":{"original_text":"The source author's exact retained observation."}},"privacy":{"requires_redaction":true}}`)
	got, err := decodeAgentOutcomeOriginal(raw, "source-snapshot", "wire")
	if err != nil || got.Original.Text != "The source author's exact retained observation." || !got.Original.RequiresRedaction {
		t.Fatalf("retained source precedence or privacy changed: %v", err)
	}
	if strings.Contains(got.Original.Text, "Generated") {
		t.Fatal("generated wrapper text became original evidence")
	}
}

func TestIndexedAgentOriginalNeverFallsBackToGeneratedWrappers(t *testing.T) {
	for _, source := range []string{"twitter:123", "bluesky:at://fixture/post", "github_issue:42", "at://fixture/app.bsky.feed.post/id"} {
		for _, truncated := range []bool{false, true} {
			metadata := map[string]any{"source_author": "@source_author", "source_relation": "quote"}
			if truncated {
				metadata["original_text"], metadata["original_text_truncated"] = "Partial retained text", true
			}
			raw, _ := json.Marshal(map[string]any{"title": "Generated wrapper", "description": "Generated wrapper says I observed an incident.",
				"digital_context": map[string]any{"metadata": metadata}})
			got, err := decodeAgentOutcomeOriginal(raw, source, "wire")
			if err != nil || got.Original.Text != "" || got.Original.Reason == "" || got.Original.SourceType == "machine_submission" ||
				got.Original.SourceAuthor != "source_author" || got.Original.SourceRelation != "quote" {
				t.Fatalf("indexed original lost strict source semantics: source=%s truncated=%v err=%v", source, truncated, err)
			}
		}
	}
	for _, key := range []string{"source_author", "author_username", "author_handle"} {
		raw, _ := json.Marshal(map[string]any{"description": "Generated wrapper", "digital_context": map[string]any{"metadata": map[string]any{"original_text": "The author's complete observation.", key: "@public_handle", "source_relation": "reply"}}})
		got, err := decodeAgentOutcomeOriginal(raw, "twitter:123", "wire")
		if err != nil || got.Original.Text != "The author's complete observation." || got.Original.SourceType != "twitter" ||
			got.Original.SourceAuthor != "public_handle" || got.Original.SourceRelation != "reply" {
			t.Fatalf("retained source author context was lost: %v", err)
		}
	}
	truncatedUnknown := []byte(`{"description":"Generated wrapper","digital_context":{"metadata":{"original_text":"Partial source","original_text_truncated":true}}}`)
	got, err := decodeAgentOutcomeOriginal(truncatedUnknown, "opaque-source-id", "wire")
	if err != nil || got.Original.Text != "" || got.Original.Reason == "" {
		t.Fatalf("explicitly truncated snapshot became wrapper evidence: %v", err)
	}
}

func TestAgentOriginalRejectsMissingMalformedOversizedOrNonMachineLaneProjection(t *testing.T) {
	oversized, _ := json.Marshal(map[string]string{"description": strings.Repeat("x", agentOriginalProjectionLimit)})
	for _, tc := range []struct {
		raw     []byte
		channel string
	}{
		{nil, "wire"}, {[]byte(`null`), "wire"}, {[]byte(`{}`), "web"}, {[]byte(`{"description":`), "wire"},
		{oversized, "public_agent"}, {[]byte(`{"description":42}`), "wire"},
	} {
		if got, err := decodeAgentOutcomeOriginal(tc.raw, "source", tc.channel); err == nil || got != nil {
			t.Fatal("invalid retained projection was accepted")
		}
	}
}

func TestAgentOriginalDatabaseHydrationBindsCanonicalFetcherAndExactWireParent(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint("null_projection=", missing), func(t *testing.T) {
			matcher := sqlmock.QueryMatcherFunc(func(stage, query string) error {
				query = strings.Join(strings.Fields(query), " ")
				for _, guard := range []string{"w.actor_kind='machine'", "w.channel IN ('wire','public_agent')", "w.fetcher_id=rr.fetcher_id"} {
					if !strings.Contains(query, guard) && !(guard == "w.fetcher_id=rr.fetcher_id" && strings.Contains(query, "rr.fetcher_id=w.fetcher_id")) {
						return fmt.Errorf("missing canonical original guard")
					}
				}
				if stage == "metadata" {
					if !strings.Contains(query, "ORDER BY w.submitted_at DESC,w.submission_id DESC LIMIT 1") {
						return fmt.Errorf("source selection disagrees with the outcome store")
					}
				} else if !strings.Contains(query, "OCTET_LENGTH(original_projection)<=65536") ||
					!strings.Contains(query, "w.submission_id=? AND w.report_seq=?") || strings.Contains(query, "$.image_base64") {
					return fmt.Errorf("unbounded or wrongly parented original hydration")
				}
				return nil
			})
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("metadata").WithArgs(84).WillReturnRows(sqlmock.NewRows([]string{"submission_id", "source_id", "channel"}).AddRow("wire-row", "source-id", "public_agent"))
			row := sqlmock.NewRows([]string{"original_projection"})
			if missing {
				row.AddRow(nil)
			} else {
				row.AddRow(`{"title":"Checkout failure","description":"The full source observation.","digital_context":{"url":"https://shop.example/checkout"}}`)
			}
			mock.ExpectQuery("projection").WithArgs("wire-row", 84).WillReturnRows(row)
			got, err := (&Database{db: db}).GetAgentOutcomeOriginal(context.Background(), 84)
			if (err != nil) != missing || (!missing && (got == nil || got.Original.SourceID != "source-id")) {
				t.Fatalf("retained projection binding: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
