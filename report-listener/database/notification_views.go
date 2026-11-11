package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Viewed links, phase 2 of docs/architecture/notification-outcomes.md.
//
// A report link in a notification email is /r/<signed token>. When it is
// followed, the handler verifies the token and calls RecordNotificationView,
// which answers "where does this link go" and, when the request looks like a
// person, appends one `viewed` row to notify_outcomes.

// NotificationViewTarget is the public report a verified link points to.
type NotificationViewTarget struct {
	PublicID string
	Digital  bool
}

// notificationViewScannerGrace: mail security gateways fetch every link the
// moment a message arrives. A request this soon after the send is a machine.
const notificationViewScannerGrace = 2 * time.Second

const notificationViewTargetSQL = `
 SELECT r.public_id, m.recipient_email, m.sent_at,
        COALESCE((SELECT ra.classification FROM report_analysis ra WHERE ra.seq = r.seq LIMIT 1), '')
 FROM notification_messages m
 JOIN notification_message_reports mr ON mr.notification_id = m.notification_id
 JOIN reports r ON r.seq = mr.seq
 WHERE m.notification_id = ? AND mr.seq = ? AND ` + publicReportCurrentVisibilitySQL + ` LIMIT 1`

// One view per notification and report per hour. The guard and the insert
// are a single statement so concurrent requests cannot both pass the check
// in the common case; a rare duplicate row is harmless (the ledger is
// append-only and readers treat `viewed` as "at least once").
const notificationViewInsertSQL = `
 INSERT INTO notify_outcomes (subject_kind, subject_ref, endpoint_key, outcome_type, source_type, source_ref, evidence_json)
 SELECT 'report', ?, ?, 'viewed', 'notification_link', ?, JSON_OBJECT('notification_id', ?)
 FROM DUAL WHERE NOT EXISTS (
   SELECT 1 FROM notify_outcomes
   WHERE subject_kind = 'report' AND subject_ref = ? AND outcome_type = 'viewed'
     AND source_ref = ? AND recorded_at > NOW() - INTERVAL 1 HOUR)`

// RecordNotificationView resolves a verified link token. It returns
// (nil, nil) when the notification did not carry that report or the report
// is no longer public: the caller must then redirect somewhere neutral and
// reveal nothing. likelyHuman is the caller's judgement of the request
// (user agent); a `viewed` row is written only when it is true and the
// request is not inside the scanner grace period. A failed write never
// fails the redirect: the target is still returned with the error.
func (d *Database) RecordNotificationView(ctx context.Context, notificationID string, seq int64, likelyHuman bool, now time.Time) (*NotificationViewTarget, error) {
	var publicID, recipient, classification string
	var sentAt time.Time
	err := d.db.QueryRowContext(ctx, notificationViewTargetSQL, notificationID, seq).Scan(&publicID, &recipient, &sentAt, &classification)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve notification link: %w", err)
	}
	target := &NotificationViewTarget{PublicID: publicID, Digital: classification == "digital"}
	if !likelyHuman || now.Sub(sentAt) < notificationViewScannerGrace {
		return target, nil
	}
	if _, err := d.db.ExecContext(ctx, notificationViewInsertSQL,
		publicID, strings.ToLower(strings.TrimSpace(recipient)), notificationID, notificationID, publicID, notificationID); err != nil {
		return target, fmt.Errorf("record notification view: %w", err)
	}
	return target, nil
}
