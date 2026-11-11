package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"report-listener/models"
)

// Outcome timeline, phase 3 of docs/architecture/notification-outcomes.md.
//
// The public notification summary says where a report was sent. The timeline
// says what came back: viewed, acknowledged, replied, bounced, no response,
// fixed. It is built only from recorded notify_outcomes rows and is free for
// everyone, including the reporter (decision of 2026-11-11).

// publicOutcomeTypes is an allowlist: an outcome type that is not listed here
// is never published, so a new internal outcome cannot leak by default.
// "sent" is omitted because the summary's events already cover it.
var publicOutcomeTypes = map[string]bool{
	"viewed": true, "acknowledged": true, "replied": true, "bounced": true,
	"no_response": true, "fixed": true, "out_of_office": true,
}

// Only the ticket reference is read from evidence_json. Message bodies,
// sender addresses and notification IDs stay private.
const reportOutcomeTimelineSQL = `
 SELECT outcome_type, recorded_at,
        COALESCE(JSON_UNQUOTE(JSON_EXTRACT(evidence_json, '$.ticket_ref')), '')
 FROM notify_outcomes
 WHERE subject_kind = 'report' AND subject_ref = ?
 ORDER BY recorded_at, id LIMIT 200`

const reportOutcomeTimelineLimit = 50

// ReportOutcomeTimeline returns the public outcome events for one report in
// time order. Repeats of the same outcome (several views, say) collapse into
// the first occurrence with a count, so the list reads as a story.
func (d *Database) ReportOutcomeTimeline(ctx context.Context, publicID string) ([]models.ReportOutcomeEvent, error) {
	rows, err := d.db.QueryContext(ctx, reportOutcomeTimelineSQL, publicID)
	if err != nil {
		return nil, fmt.Errorf("query report outcomes: %w", err)
	}
	defer rows.Close()
	timeline := []models.ReportOutcomeEvent{}
	index := map[string]int{}
	for rows.Next() {
		var outcome, ticket string
		var at sql.NullTime
		if err := rows.Scan(&outcome, &at, &ticket); err != nil {
			return nil, fmt.Errorf("scan report outcome: %w", err)
		}
		outcome = strings.ToLower(strings.TrimSpace(outcome))
		if !publicOutcomeTypes[outcome] || !at.Valid {
			continue
		}
		ticket = publicTicketReference(ticket)
		if i, seen := index[outcome]; seen {
			timeline[i].Count++
			if timeline[i].TicketRef == "" {
				timeline[i].TicketRef = ticket
			}
			continue
		}
		if len(timeline) >= reportOutcomeTimelineLimit {
			continue
		}
		index[outcome] = len(timeline)
		timeline = append(timeline, models.ReportOutcomeEvent{Type: outcome, At: at.Time.UTC(), Count: 1, TicketRef: ticket})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read report outcomes: %w", err)
	}
	return timeline, nil
}

// publicTicketReference keeps a ticket reference only if it looks like one:
// short, and letters, digits, '-' or '#' only. Inbound mail is untrusted, so
// anything else is dropped rather than shown on a public page.
func publicTicketReference(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 24 {
		return ""
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '#') {
			return ""
		}
	}
	return value
}
