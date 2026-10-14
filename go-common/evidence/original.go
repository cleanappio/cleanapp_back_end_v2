package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Original describes a retained source, never an analyzer-generated summary.
type Original struct {
	Image             []byte
	Text              string
	SourceType        string
	SourceID          string
	RequiresRedaction bool
	Reason            string
}

func SelectOriginal(source, description, sharedText, upstreamText string) Original {
	result := Original{SourceType: source}
	switch source {
	case "", "human_submission":
		result.Text = description
	case "digital_share":
		result.Text = sharedText
	default:
		result.Text = upstreamText
	}
	if strings.EqualFold(strings.TrimSpace(result.Text), "Human report submission") {
		result.Text = ""
	}
	if strings.TrimSpace(result.Text) == "" {
		result.Text = ""
		result.Reason = "Stored original text is unavailable; generated summaries are not used for classification."
	}
	return result
}

func WireSourceIdentity(sourceID string) (string, string) {
	if strings.HasPrefix(sourceID, "at://") && strings.Contains(sourceID, "/app.bsky.feed.post/") {
		return "bluesky", sourceID
	}
	for _, source := range []string{"github_issue", "twitter", "bluesky"} {
		if id, ok := strings.CutPrefix(sourceID, source+":"); ok && id != "" {
			return source, id
		}
	}
	return "unknown", ""
}

// LoadOriginal follows the deterministic intake identity. Missing optional
// indexer tables/rows leave imports unknown, never using generated descriptions.
func LoadOriginal(ctx context.Context, db *sql.DB, seq int) (Original, error) {
	var description sql.NullString
	var image []byte
	if err := db.QueryRowContext(ctx, `SELECT description, image FROM reports WHERE seq = ?`, seq).Scan(&description, &image); err != nil {
		return Original{}, fmt.Errorf("load original report %d: %w", seq, err)
	}
	var source, id string
	err := db.QueryRowContext(ctx, `SELECT source, external_id FROM external_ingest_index WHERE seq = ? ORDER BY source, external_id LIMIT 1`, seq).Scan(&source, &id)
	if err != nil && err != sql.ErrNoRows && !missingTable(err) {
		return Original{}, fmt.Errorf("load original identity: %w", err)
	}
	// When intake tables are absent, provenance is unavailable; do not treat
	// an arbitrary stored description as a direct human submission.
	identityUnavailable := missingTable(err)
	var rawSourceID, sourceType sql.NullString
	rawErr := db.QueryRowContext(ctx, `SELECT source_id, source_type FROM report_raw WHERE report_seq = ?`, seq).Scan(&rawSourceID, &sourceType)
	if rawErr != nil && rawErr != sql.ErrNoRows && !missingTable(rawErr) && !missingColumn(rawErr) {
		return Original{}, fmt.Errorf("load original wire identity: %w", rawErr)
	}
	if source == "" && (identityUnavailable || missingTable(rawErr) || missingColumn(rawErr)) {
		source = "unknown"
	}
	if source == "" && rawErr == nil {
		source, id = WireSourceIdentity(rawSourceID.String)
	}

	// Wire's actor_kind and channel are persisted by the authenticated ingress
	// adapter. Model output and caller-supplied agent metadata cannot authorize a
	// description fallback for a machine import.
	var reportJSON []byte
	var actorKind, channel string
	wireErr := db.QueryRowContext(ctx, `SELECT report_json, actor_kind, channel FROM wire_submissions_raw WHERE report_seq = ? ORDER BY created_at DESC LIMIT 1`, seq).Scan(&reportJSON, &actorKind, &channel)
	if wireErr != nil && wireErr != sql.ErrNoRows && !missingTable(wireErr) {
		return Original{}, fmt.Errorf("load retained wire original: %w", wireErr)
	}
	native := nativeHumanOriginal(actorKind, channel, reportJSON)
	if native.SourceType != "" {
		source = native.SourceType
		id = rawSourceID.String
	}
	if source == "" {
		source = "unknown"
	}
	var shared sql.NullString
	if source == "digital_share" {
		err = db.QueryRowContext(ctx, `SELECT shared_text FROM digital_share_reports WHERE report_seq = ?`, seq).Scan(&shared)
		if err != nil && err != sql.ErrNoRows && !missingTable(err) {
			return Original{}, fmt.Errorf("load shared original: %w", err)
		}
		if strings.TrimSpace(shared.String) == "" {
			shared.String = native.Text
		}
		// The dedicated shared_text field is the original; the display description
		// adds synthetic URL/source-app prefixes and must not become source evidence.
		result := SelectOriginal(source, "", shared.String, "")
		result.Image = image
		result.SourceID = id
		result.RequiresRedaction = RequiresRedaction(reportJSON)
		return result, nil
	}
	if source == "human_submission" {
		annotation := native.Text
		if strings.TrimSpace(annotation) == "" {
			annotation = description.String
		}
		result := SelectOriginal(source, annotation, "", "")
		result.Image = image
		result.SourceID = id
		result.RequiresRedaction = RequiresRedaction(reportJSON)
		return result, nil
	}
	// Prefer complete retained intake snapshots over mutable indexer rows.
	if wireErr == nil {
		if retained := RetainedOriginal(reportJSON); strings.TrimSpace(retained) != "" {
			return Original{Text: retained, Image: image, RequiresRedaction: RequiresRedaction(reportJSON), SourceType: source, SourceID: id}, nil
		}
	}
	var upstream sql.NullString
	switch source {
	case "github_issue":
		err = db.QueryRowContext(ctx, `SELECT CONCAT(COALESCE(title, ''), '\n\n', COALESCE(body, '')) FROM indexer_github_issue WHERE issue_id = ?`, id).Scan(&upstream)
	case "twitter", "x":
		err = db.QueryRowContext(ctx, `SELECT text FROM indexer_twitter_tweet WHERE tweet_id = ?`, id).Scan(&upstream)
	case "bluesky":
		err = db.QueryRowContext(ctx, `SELECT text FROM indexer_bluesky_post WHERE uri = ?`, id).Scan(&upstream)
	default:
		err = nil
	}
	if err != nil && err != sql.ErrNoRows && !missingTable(err) {
		return Original{}, fmt.Errorf("load upstream original: %w", err)
	}
	result := SelectOriginal(source, description.String, shared.String, upstream.String)
	result.SourceID = id
	result.Image = image
	result.RequiresRedaction = RequiresRedaction(reportJSON)
	return result, nil
}

func missingTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "doesn't exist")
}
func missingColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unknown column")
}

// RetainedOriginal reads only the explicit immutable original field, never
// report.description, report.title, or inferred content in the Wire payload.
func RetainedOriginal(reportJSON []byte) string {
	var payload struct {
		DigitalContext struct {
			Metadata struct {
				OriginalText          string `json:"original_text"`
				OriginalTextTruncated bool   `json:"original_text_truncated"`
			} `json:"metadata"`
		} `json:"digital_context"`
	}
	if json.Unmarshal(reportJSON, &payload) != nil {
		return ""
	}
	if payload.DigitalContext.Metadata.OriginalTextTruncated {
		return ""
	}
	return payload.DigitalContext.Metadata.OriginalText
}

// RequiresRedaction is independent of the experience label. Public readers must
// omit raw quotes/images whenever the intake privacy flags require redaction.
func RequiresRedaction(reportJSON []byte) bool {
	var payload struct {
		Privacy struct {
			ContainsPII       bool `json:"contains_pii"`
			RequiresRedaction bool `json:"requires_redaction"`
		} `json:"privacy"`
	}
	if len(reportJSON) == 0 {
		return false
	}
	if json.Unmarshal(reportJSON, &payload) != nil {
		return true
	}
	return payload.Privacy.ContainsPII || payload.Privacy.RequiresRedaction
}

// nativeHumanOriginal recognizes only server-authenticated native ingestion
// paths. An imported/generated report cannot opt in by using a human-looking ID
// or by claiming agent.operator_type=human in its own payload.
func nativeHumanOriginal(actorKind, channel string, reportJSON []byte) Original {
	if actorKind != "human" || (channel != "mobile" && channel != "web" && channel != "legacy_v2") {
		return Original{}
	}
	var payload struct {
		ProblemType    string `json:"problem_type"`
		Description    string `json:"description"`
		DigitalContext struct {
			SharedText string `json:"shared_text"`
		} `json:"digital_context"`
	}
	if json.Unmarshal(reportJSON, &payload) != nil {
		return Original{}
	}
	switch payload.ProblemType {
	case "citizen_submission":
		return Original{Text: payload.Description, SourceType: "human_submission"}
	case "shared_content_report":
		return Original{Text: payload.DigitalContext.SharedText, SourceType: "digital_share"}
	default:
		return Original{}
	}
}
