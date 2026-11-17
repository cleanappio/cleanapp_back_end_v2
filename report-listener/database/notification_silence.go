package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"report-listener/models"
)

// Silence, the first half of phase 5 of
// docs/architecture/notification-outcomes.md: a notification that got no
// bounce, view, acknowledgement or reply within the waiting period is
// recorded as `no_response`.
//
// "No response" is only true if replies would have been seen. So a
// notification is judged only when the inbox worker was already reading mail
// for it: it was sent no earlier than the lookback window before the worker's
// first handled message. Before the inbox worker has handled anything,
// nothing is judged.

// silenceResponseTypes are the outcomes that mean something came back. An
// absence notice is not one of them.
// "no_response" is last: it is in the list so a notification is judged once.
var silenceResponseTypes = []string{"bounced", "viewed", "acknowledged", "replied", "unsubscribed", "no_response"}

// AddWorkingDays returns t moved forward (or back, for a negative n) by n
// Monday-to-Friday days. Public holidays are not known and count as working.
func AddWorkingDays(t time.Time, n int) time.Time {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for n > 0 {
		t = t.AddDate(0, 0, step)
		if day := t.Weekday(); day != time.Saturday && day != time.Sunday {
			n--
		}
	}
	return t
}

// silenceScanWindow bounds how far back a pass looks for unanswered
// notifications. One older than this that was never judged stays unjudged.
const silenceScanWindow = 45 * 24 * time.Hour

// silenceCandidateLimit bounds one pass.
const silenceCandidateLimit = 500

// SilenceCandidates lists notifications sent at or before sentBefore, within
// the scan window, that the inbox worker could have seen a reply to (see the
// note at the top of this file). It does not check what came back:
// RecordNoResponse does, for each one.
//
// The check is done per notification with parameters, not as a join, for two
// reasons: notify_outcomes is indexed by subject, and the tables involved do
// not share a collation, so MySQL refuses to compare their columns directly.
func (d *Database) SilenceCandidates(ctx context.Context, sentBefore time.Time, inboxLookback time.Duration) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT m.notification_id
		FROM notification_messages m
		WHERE m.sent_at >= (SELECT DATE_SUB(MIN(processed_at), INTERVAL ? SECOND) FROM inbox_messages)
		  AND m.sent_at >= ? AND m.sent_at <= ?
		ORDER BY m.sent_at LIMIT ?`,
		int64(inboxLookback/time.Second), sentBefore.Add(-silenceScanWindow), sentBefore, silenceCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("list silence candidates: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecordNoResponse writes one `no_response` row per report the notification
// carried, unless something came back or it was recorded before, and counts
// it against the address. It returns how many rows it wrote. Each insert
// re-checks, so a reply that arrives during the pass wins.
func (d *Database) RecordNoResponse(ctx context.Context, notificationID string, waitingWorkingDays int, now time.Time) (int64, error) {
	notification, err := d.NotificationByID(ctx, notificationID)
	if err != nil || notification == nil || len(notification.PublicIDs) == 0 {
		return 0, err
	}
	recipient := strings.ToLower(strings.TrimSpace(notification.Recipient))
	typeMarks := strings.TrimSuffix(strings.Repeat("?,", len(silenceResponseTypes)), ",")
	refMarks := strings.TrimSuffix(strings.Repeat("?,", len(notification.PublicIDs)), ",")
	// Something back for any report of the notification answers all of them.
	guard := `SELECT 1 FROM notify_outcomes
		WHERE subject_kind = 'report' AND subject_ref IN (` + refMarks + `) AND source_ref = ? AND outcome_type IN (` + typeMarks + `)`
	guardArgs := []any{}
	for _, publicID := range notification.PublicIDs {
		guardArgs = append(guardArgs, publicID)
	}
	guardArgs = append(guardArgs, notification.ID)
	for _, outcome := range silenceResponseTypes {
		guardArgs = append(guardArgs, outcome)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin no response: %w", err)
	}
	defer tx.Rollback()
	var written int64
	for i, publicID := range notification.PublicIDs {
		if i > 0 {
			// Rows written by this call must not block the remaining reports.
			guardArgs[len(guardArgs)-1] = "" // drops "no_response" from the guard
		}
		args := append([]any{publicID, recipient, notification.ID, waitingWorkingDays, notification.ID, now}, guardArgs...)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO notify_outcomes (subject_kind, subject_ref, endpoint_key, outcome_type, source_type, source_ref, evidence_json, recorded_at)
			SELECT 'report', ?, ?, 'no_response', 'silence', ?, JSON_OBJECT('waiting_working_days', ?, 'notification_id', ?), ?
			FROM DUAL WHERE NOT EXISTS (`+guard+`)`, args...)
		if err != nil {
			return 0, fmt.Errorf("record no response: %w", err)
		}
		n, _ := result.RowsAffected()
		if i == 0 && n == 0 {
			return 0, nil // something came back, or already recorded
		}
		written += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit no response: %w", err)
	}
	if written == 0 || recipient == "" {
		return written, nil
	}
	key := "email:" + recipient
	memory := models.ContactEndpointMemory{EndpointKey: key, ChannelType: "email", ChannelValue: recipient}
	existing, err := d.GetContactEndpointMemory(ctx, key)
	if err != nil {
		return written, fmt.Errorf("read endpoint memory: %w", err)
	}
	if existing != nil {
		memory = *existing
	}
	memory.LastResult = "no_response"
	memory.NoResponseCount++
	if _, err := d.UpsertContactEndpointMemory(ctx, memory); err != nil {
		return written, fmt.Errorf("update endpoint memory: %w", err)
	}
	return written, nil
}
