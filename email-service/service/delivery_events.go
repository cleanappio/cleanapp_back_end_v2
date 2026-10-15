package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"email-service/email"
)

// recordReportEmailDelivery also records the legacy single-report send paths.
// A "sent" event means accepted by the configured transport, not inbox delivery.
func (s *EmailService) recordReportEmailDelivery(ctx context.Context, seq int64, recipient, source string) error {
	return s.recordReportEmailDeliveryResult(ctx, seq, recipient, source, email.CustomEmailSendResult{Provider: s.email.Provider(), Status: "sent", SentAt: time.Now().UTC()})
}

func (s *EmailService) recordReportEmailDeliveryResult(ctx context.Context, seq int64, recipient, source string, result email.CustomEmailSendResult) error {
	if result.Status != "sent" || result.SentAt.IsZero() {
		return fmt.Errorf("refusing notification event without accepted send")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}
		if lastErr = s.recordReportEmailDeliveryAttempt(ctx, seq, recipient, source, result); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (s *EmailService) recordReportEmailDeliveryAttempt(ctx context.Context, seq int64, recipient, source string, result email.CustomEmailSendResult) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	sentAt := result.SentAt.UTC()
	key := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", seq, recipient, result.Provider, result.ProviderMessageID, sentAt.Format(time.RFC3339Nano))))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Snapshot the prior legacy row before its UNIQUE(seq,recipient) upsert can
	// replace it. Old digest records retain their original source and date.
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO report_email_delivery_events
		(seq,recipient_email,delivery_status,delivery_source,provider,sent_at,legacy_delivery_id,is_legacy)
		SELECT old.seq,old.recipient_email,old.delivery_status,old.delivery_source,old.provider,old.sent_at,old.id,TRUE
		FROM report_email_deliveries old WHERE old.seq=? AND old.recipient_email=?
		AND NOT EXISTS (SELECT 1 FROM report_email_delivery_events e WHERE e.seq=old.seq
		AND e.recipient_email COLLATE utf8mb4_unicode_ci=old.recipient_email COLLATE utf8mb4_unicode_ci
		AND e.delivery_source COLLATE utf8mb4_unicode_ci=old.delivery_source COLLATE utf8mb4_unicode_ci AND e.sent_at<=>old.sent_at)`, seq, recipient)
	if err != nil {
		return fmt.Errorf("preserve prior notification event: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO report_email_delivery_events
		(seq,recipient_email,delivery_status,delivery_source,provider,provider_message_id,sent_at,is_legacy,receipt_key)
		VALUES (?,?,'sent',?,?,NULLIF(?,''),?,FALSE,?) ON DUPLICATE KEY UPDATE id=id`, seq, recipient, source, result.Provider, result.ProviderMessageID, sentAt, fmt.Sprintf("%x", key))
	if err != nil {
		return fmt.Errorf("append notification event: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO report_email_deliveries
		(seq,recipient_email,delivery_status,delivery_source,provider,sent_at)
		VALUES (?,?,'sent',?,?,?) ON DUPLICATE KEY UPDATE delivery_status=VALUES(delivery_status),
		delivery_source=VALUES(delivery_source),provider=VALUES(provider),sent_at=VALUES(sent_at),updated_at=NOW()`, seq, recipient, source, result.Provider, sentAt)
	if err != nil {
		return fmt.Errorf("update latest notification delivery: %w", err)
	}
	return tx.Commit()
}

// Verify schema and INSERT permissions before sending anything externally.
// Zero-row INSERTs and a rollback leave no fabricated notification records.
func (s *EmailService) preflightNotificationReceipts(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO report_email_delivery_events (seq,recipient_email,delivery_status,delivery_source,provider,provider_message_id,sent_at,is_legacy,legacy_delivery_id,receipt_key)
		SELECT 0,'','sent','aggregate_report','',NULL,NOW(),FALSE,NULL,NULL WHERE FALSE`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO report_email_deliveries (seq,recipient_email,delivery_status,delivery_source,provider,sent_at)
		SELECT 0,'','sent','aggregate_report','',NOW() WHERE FALSE`); err != nil {
		return err
	}
	return tx.Rollback()
}
