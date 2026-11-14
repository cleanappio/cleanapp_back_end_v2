package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"cleanapp-common/evidence"
)

const agentOriginalProjectionLimit = 65536

// AgentOutcomeOriginal keeps the literal observation distinct from supplied
// product/place/source context. Neither makes a machine a verified human author.
type AgentOutcomeOriginal struct {
	Original evidence.Original
	Context  json.RawMessage
	Channel  string
}

// GetAgentOutcomeOriginal follows the same canonical fetcher and Wire ordering
// as the shared outcome store. Select narrow identity metadata before hydrating
// bounded text/context; inline image bytes never cross this query boundary.
func (d *Database) GetAgentOutcomeOriginal(ctx context.Context, seq int) (*AgentOutcomeOriginal, error) {
	var submissionID, sourceID, channel string
	err := d.db.QueryRowContext(ctx, `SELECT w.submission_id,w.source_id,w.channel
		FROM report_raw rr JOIN wire_submissions_raw w
		ON w.report_seq=rr.report_seq AND w.fetcher_id=rr.fetcher_id
		WHERE rr.report_seq=? AND w.actor_kind='machine' AND w.channel IN ('wire','public_agent')
		ORDER BY w.submitted_at DESC,w.submission_id DESC LIMIT 1`, seq).
		Scan(&submissionID, &sourceID, &channel)
	if err != nil {
		return nil, fmt.Errorf("select retained agent original: %w", err)
	}
	var raw []byte
	err = d.db.QueryRowContext(ctx, `SELECT CASE WHEN OCTET_LENGTH(original_projection)<=65536
		THEN original_projection ELSE NULL END FROM (
		 SELECT JSON_OBJECT(
		 'title',JSON_EXTRACT(w.report_json,'$.title'),
		 'description',JSON_EXTRACT(w.report_json,'$.description'),
		 'domain',JSON_EXTRACT(w.report_json,'$.domain'),
		 'problem_type',JSON_EXTRACT(w.report_json,'$.problem_type'),
		 'target_entity',JSON_EXTRACT(w.report_json,'$.target_entity'),
		 'digital_context',JSON_EXTRACT(w.report_json,'$.digital_context'),
		 'location',JSON_EXTRACT(w.report_json,'$.location'),
		 'evidence_bundle',JSON_EXTRACT(w.report_json,'$.evidence_bundle'),
		 'privacy',JSON_EXTRACT(w.report_json,'$.privacy')) AS original_projection
		 FROM wire_submissions_raw w JOIN report_raw rr
		 ON rr.report_seq=w.report_seq AND rr.fetcher_id=w.fetcher_id
		 WHERE w.submission_id=? AND w.report_seq=? AND w.actor_kind='machine'
		 AND w.channel IN ('wire','public_agent')
		) retained`, submissionID, seq).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("hydrate retained agent original: %w", err)
	}
	return decodeAgentOutcomeOriginal(raw, sourceID, channel)
}

func decodeAgentOutcomeOriginal(raw []byte, sourceID, channel string) (*AgentOutcomeOriginal, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > agentOriginalProjectionLimit ||
		(channel != "wire" && channel != "public_agent") || !json.Valid(raw) || len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("retained agent original is unavailable or exceeds the bounded projection")
	}
	var report struct {
		Title          string `json:"title"`
		Description    string `json:"description"`
		DigitalContext struct {
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"digital_context"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("invalid retained agent text projection")
	}
	// Existing indexer snapshots retain the public author's exact original in
	// metadata.original_text. A generated wrapper description cannot replace it.
	text := evidence.RetainedOriginal(raw)
	sourceType, externalID := evidence.WireSourceIdentity(sourceID)
	author, relation := "", "unknown"
	if sourceType != "unknown" {
		// Acquiring an indexed public observation through a machine does not
		// make its generated wrapper the source author's original evidence.
		author, relation = retainedAgentSourceContext(raw)
		sourceID = externalID
	} else {
		sourceType = "machine_submission"
	}
	_, explicitOriginal := report.DigitalContext.Metadata["original_text"]
	_, explicitTruncation := report.DigitalContext.Metadata["original_text_truncated"]
	if strings.TrimSpace(text) == "" && sourceType == "machine_submission" && !explicitOriginal && !explicitTruncation {
		text = strings.TrimSpace(report.Description)
		title := strings.TrimSpace(report.Title)
		if text == "" {
			text = title
		} else if title != "" && !strings.HasPrefix(text, title) {
			text = title + "\n\n" + text
		}
	}
	reason := ""
	if strings.TrimSpace(text) == "" {
		text = ""
		reason = "Stored original text is unavailable; generated summaries are not used for classification."
	}
	return &AgentOutcomeOriginal{Original: evidence.Original{
		Text: text, SourceType: sourceType, SourceID: sourceID,
		SourceAuthor: author, SourceRelation: relation, RequiresRedaction: evidence.RequiresRedaction(raw), Reason: reason,
	}, Context: append(json.RawMessage(nil), raw...), Channel: channel}, nil
}

// Match the existing evidence source-context rules: only bounded public handles
// and recognized post relationships, never a caller's claimed human identity.
func retainedAgentSourceContext(raw []byte) (string, string) {
	var report struct {
		DigitalContext struct {
			AuthorHandle string `json:"author_handle"`
			Metadata     struct {
				SourceAuthor   string `json:"source_author"`
				AuthorUsername string `json:"author_username"`
				AuthorHandle   string `json:"author_handle"`
				SourceRelation string `json:"source_relation"`
			} `json:"metadata"`
		} `json:"digital_context"`
	}
	if json.Unmarshal(raw, &report) != nil {
		return "", "unknown"
	}
	meta := report.DigitalContext.Metadata
	author := ""
	for _, value := range []string{meta.SourceAuthor, meta.AuthorUsername, meta.AuthorHandle, report.DigitalContext.AuthorHandle} {
		value = strings.TrimPrefix(strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, value)), "@")
		valid := value != ""
		for _, r := range value {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' && r != '-' {
				valid = false
				break
			}
		}
		if valid {
			author = evidence.Bound(value, 253)
			break
		}
	}
	relation := strings.ToLower(strings.TrimSpace(meta.SourceRelation))
	switch relation {
	case "quote", "reply", "retweet", "original":
	default:
		relation = "unknown"
	}
	return author, relation
}
