package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"report-listener/models"
)

// A sequence-indexed union retains legacy receipts without double-counting
// receipts already captured in the append-only ledger. Legacy rows only retain
// the latest send per recipient, so their presence makes history incomplete.
const reportNotificationRowsSQL = `
 SELECT recipient_email, delivery_source, sent_at, is_legacy
 FROM report_email_delivery_events
 WHERE seq = ? AND delivery_status = 'sent'
 UNION ALL
 SELECT old.recipient_email, old.delivery_source, old.sent_at, TRUE AS is_legacy
 FROM report_email_deliveries old
 WHERE old.seq = ? AND old.delivery_status = 'sent'
 AND NOT EXISTS (
   SELECT 1 FROM report_email_delivery_events e
   WHERE e.seq = old.seq AND e.delivery_status = 'sent'
     AND e.recipient_email = old.recipient_email
     AND e.delivery_source = old.delivery_source AND e.sent_at <=> old.sent_at
 )
 ORDER BY sent_at DESC`

type notificationReceipt struct {
	Email, Source string
	SentAt        *time.Time
	Legacy        bool
}

func (d *Database) PublicReportSequence(ctx context.Context, publicID string) (int, error) {
	var seq int
	err := d.db.QueryRowContext(ctx, `
 SELECT r.seq FROM reports r
 WHERE r.public_id = ?
 AND (SELECT COUNT(*) FROM report_raw rr WHERE rr.report_seq = r.seq AND rr.visibility <> 'public') = 0
 AND (SELECT COUNT(*) FROM report_status rs WHERE rs.seq = r.seq AND rs.status <> 'active') = 0
 AND (SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq = r.seq AND ro.owner IS NOT NULL AND ro.owner <> '' AND ro.is_public = FALSE) = 0
 LIMIT 1`, publicID).Scan(&seq)
	return seq, err
}

func (d *Database) HasActiveNotificationSubscription(ctx context.Context, userID string) (bool, error) {
	var active bool
	err := d.db.QueryRowContext(ctx, `
 SELECT EXISTS(SELECT 1 FROM subscriptions
 WHERE customer_id = ? AND status = 'active'
 AND plan_type IN ('base', 'advanced', 'exclusive') AND start_date <= CURRENT_DATE())`, userID).Scan(&active)
	return active, err
}

func (d *Database) ReportNotificationHistory(ctx context.Context, seq int) (*models.ReportNotificationHistory, error) {
	rows, err := d.db.QueryContext(ctx, reportNotificationRowsSQL, seq, seq)
	if err != nil {
		return nil, fmt.Errorf("query report notifications: %w", err)
	}
	defer rows.Close()
	var receipts []notificationReceipt
	for rows.Next() {
		var receipt notificationReceipt
		var sentAt sql.NullTime
		if err := rows.Scan(&receipt.Email, &receipt.Source, &sentAt, &receipt.Legacy); err != nil {
			return nil, fmt.Errorf("scan report notification: %w", err)
		}
		if sentAt.Valid {
			t := sentAt.Time.UTC()
			receipt.SentAt = &t
		}
		receipts = append(receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read report notifications: %w", err)
	}
	return buildReportNotificationHistory(receipts), nil
}

func notificationContentKind(source string) string {
	switch source {
	case "aggregate_brand":
		return "brand_digest"
	case "aggregate_report", "inferred_contact", "area_contact":
		return "report_email"
	default:
		return "notification"
	}
}

// Only generic role mailboxes on non-consumer domains qualify for public preview.
// Never publish inferred personal addresses, even if they were notification recipients.
func publicNotificationMailbox(email string) bool {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || !strings.Contains(parts[1], ".") {
		return false
	}
	switch parts[1] {
	case "gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "live.com", "yahoo.com", "icloud.com", "me.com", "aol.com", "proton.me", "protonmail.com":
		return false
	}
	switch parts[0] {
	case "support", "security", "contact", "info", "help", "press", "feedback", "abuse", "privacy", "hello", "bugs", "bugreport":
		return true
	default:
		return false
	}
}

func mailboxPriority(email string) int {
	for priority, role := range []string{"support", "security", "contact", "info", "help", "feedback", "bugs", "bugreport", "abuse", "privacy", "press", "hello"} {
		if strings.HasPrefix(email, role+"@") {
			return priority
		}
	}
	return 100
}

func buildReportNotificationHistory(receipts []notificationReceipt) *models.ReportNotificationHistory {
	history := &models.ReportNotificationHistory{
		ReportNotificationSummary: models.ReportNotificationSummary{
			State: "none_recorded", HistoryComplete: len(receipts) > 0, FullLogRequiresSubscription: true,
			PreviewRecipients: []string{}, Events: []models.ReportNotificationEvent{}, ContentKind: "notification",
		},
		Recipients: []models.ReportNotificationRecipient{},
	}
	recipients := map[string]bool{}
	groups := map[string]int{}
	kinds := map[string]bool{}
	for _, receipt := range receipts {
		email := strings.ToLower(strings.TrimSpace(receipt.Email))
		if email == "" { // malformed legacy rows cannot establish an email send
			continue
		}
		kind := notificationContentKind(receipt.Source)
		history.EmailCount++
		recipients[email] = true
		kinds[kind] = true
		history.HistoryComplete = history.HistoryComplete && !receipt.Legacy
		if receipt.SentAt != nil && (history.LatestSentAt == nil || receipt.SentAt.After(*history.LatestSentAt)) {
			history.LatestSentAt = receipt.SentAt
		}
		key := kind + ":unknown"
		if receipt.SentAt != nil {
			key = kind + ":" + receipt.SentAt.UTC().Format(time.RFC3339Nano)
		}
		if index, exists := groups[key]; exists {
			history.Events[index].EmailCount++
		} else {
			groups[key] = len(history.Events)
			history.Events = append(history.Events, models.ReportNotificationEvent{SentAt: receipt.SentAt, EmailCount: 1, ContentKind: kind})
		}
		history.Recipients = append(history.Recipients, models.ReportNotificationRecipient{Email: email, SentAt: receipt.SentAt, Status: "sent", ContentKind: kind})
	}
	history.RecipientCount = len(recipients)
	preview := []string{}
	for email := range recipients {
		if publicNotificationMailbox(email) {
			preview = append(preview, email)
		}
	}
	sort.Slice(preview, func(i, j int) bool {
		if mailboxPriority(preview[i]) != mailboxPriority(preview[j]) {
			return mailboxPriority(preview[i]) < mailboxPriority(preview[j])
		}
		return preview[i] < preview[j]
	})
	if len(preview) > 1 {
		preview = preview[:1]
	}
	history.PreviewRecipients = preview
	history.HiddenRecipientCount = history.RecipientCount - len(preview)
	if history.EmailCount > 0 {
		history.State = "sent"
	}
	if len(kinds) == 1 {
		for kind := range kinds {
			history.ContentKind = kind
		}
	} else if len(kinds) > 1 {
		history.ContentKind = "mixed"
	}
	return history
}

func UnavailableNotificationSummary() *models.ReportNotificationSummary {
	return &models.ReportNotificationSummary{State: "unavailable", FullLogRequiresSubscription: true, PreviewRecipients: []string{}, Events: []models.ReportNotificationEvent{}, ContentKind: "notification"}
}
