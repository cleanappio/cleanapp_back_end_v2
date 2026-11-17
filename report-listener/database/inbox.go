package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"report-listener/inbox"
	"report-listener/models"
)

// Inbound mail, phase 1b of docs/architecture/notification-outcomes.md.
// These methods implement inbox.Store.

// How long an address is left alone after an inbound signal. The values match
// the manual case path (handlers.notifyOutcomeCooldown).
const (
	inboxPermanentBounceCooldown = 14 * 24 * time.Hour
	inboxAcknowledgedCooldown    = 72 * time.Hour
)

func ensureInboxMessagesTable(ctx context.Context, db *sql.DB) error {
	// result is the notify outcome type recorded, or 'unmatched' or
	// 'unparseable'. The row exists so a mailbox message is handled once.
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS inbox_messages (
		mailbox_message_id VARCHAR(191) NOT NULL,
		notification_id CHAR(16) NULL,
		result VARCHAR(32) NOT NULL,
		received_at TIMESTAMP NULL,
		processed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (mailbox_message_id),
		KEY idx_inbox_messages_notification (notification_id),
		KEY idx_inbox_messages_processed (processed_at)
	) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`); err != nil {
		return fmt.Errorf("failed to create inbox_messages table: %w", err)
	}
	return nil
}

func (d *Database) InboxMessageHandled(ctx context.Context, mailboxMessageID string) (bool, error) {
	var one int
	err := d.db.QueryRowContext(ctx, `SELECT 1 FROM inbox_messages WHERE mailbox_message_id = ?`, mailboxMessageID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check inbox message: %w", err)
	}
	return true, nil
}

func (d *Database) NotificationByID(ctx context.Context, id string) (*inbox.Notification, error) {
	return d.notificationWhere(ctx, `notification_id = ?`, id)
}

// NotificationByMessageID matches the Message-ID the relay assigned, which a
// bounce or a threaded reply quotes. Stored IDs may or may not carry angle
// brackets, so both forms are tried.
func (d *Database) NotificationByMessageID(ctx context.Context, messageIDs []string) (*inbox.Notification, error) {
	var args []any
	for _, id := range messageIDs {
		id = strings.Trim(strings.TrimSpace(id), "<>")
		if id != "" && len(args) < 32 {
			args = append(args, id, "<"+id+">")
		}
	}
	if len(args) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	return d.notificationWhere(ctx, `provider_message_id IN (`+placeholders+`) ORDER BY sent_at DESC`, args...)
}

func (d *Database) notificationWhere(ctx context.Context, where string, args ...any) (*inbox.Notification, error) {
	notification := &inbox.Notification{}
	err := d.db.QueryRowContext(ctx, `SELECT notification_id, recipient_email FROM notification_messages WHERE `+where+` LIMIT 1`, args...).
		Scan(&notification.ID, &notification.Recipient)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find notification: %w", err)
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT r.public_id FROM notification_message_reports mr
		JOIN reports r ON r.seq = mr.seq
		WHERE mr.notification_id = ? AND COALESCE(r.public_id, '') <> ''
		ORDER BY mr.seq LIMIT 200`, notification.ID)
	if err != nil {
		return nil, fmt.Errorf("list notification reports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			return nil, err
		}
		notification.PublicIDs = append(notification.PublicIDs, publicID)
	}
	return notification, rows.Err()
}

// RecordInboxMessage stores one handled message. The inbox_messages row, the
// outcome rows and an opt-out are one transaction keyed on the mailbox
// message ID, so a repeat (a second poll, a second replica) changes nothing.
func (d *Database) RecordInboxMessage(ctx context.Context, record inbox.Record) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin inbox record: %w", err)
	}
	defer tx.Rollback()

	var notificationID any
	if record.Notification != nil {
		notificationID = record.Notification.ID
	}
	inserted, err := tx.ExecContext(ctx, `INSERT IGNORE INTO inbox_messages (mailbox_message_id, notification_id, result, received_at) VALUES (?, ?, ?, ?)`,
		record.MailboxMessageID, notificationID, record.Result, record.ReceivedAt)
	if err != nil {
		return fmt.Errorf("record inbox message: %w", err)
	}
	if n, _ := inserted.RowsAffected(); n == 0 {
		return nil // already handled
	}
	if record.Notification == nil {
		return tx.Commit()
	}
	recipient := strings.ToLower(strings.TrimSpace(record.Notification.Recipient))
	for _, publicID := range record.Notification.PublicIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notify_outcomes (subject_kind, subject_ref, endpoint_key, outcome_type, source_type, source_ref, evidence_json, recorded_at)
			VALUES ('report', ?, ?, ?, 'inbox', ?, NULLIF(?, ''), ?)`,
			publicID, recipient, record.Result, record.Notification.ID, record.Evidence, record.ReceivedAt); err != nil {
			return fmt.Errorf("record inbox outcome: %w", err)
		}
	}
	if record.OptOut {
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO opted_out_emails (email) VALUES (?)`, recipient); err != nil {
			return fmt.Errorf("record opt-out: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit inbox record: %w", err)
	}
	// Endpoint memory is a derived summary that later routing reads. The
	// outcome rows above are the record, so a failure here is reported but
	// does not undo them.
	return d.noteInboxEndpointOutcome(ctx, recipient, record)
}

func (d *Database) noteInboxEndpointOutcome(ctx context.Context, recipient string, record inbox.Record) error {
	if recipient == "" || (record.Result != "bounced" && record.Result != "acknowledged") {
		return nil
	}
	key := "email:" + recipient
	memory := models.ContactEndpointMemory{EndpointKey: key, ChannelType: "email", ChannelValue: recipient}
	existing, err := d.GetContactEndpointMemory(ctx, key)
	if err != nil {
		return fmt.Errorf("read endpoint memory: %w", err)
	}
	if existing != nil {
		memory = *existing
	}
	memory.LastResult = record.Result
	var cooldown time.Duration
	if record.Result == "bounced" {
		memory.BounceCount++
		if record.PermanentBounce {
			cooldown = inboxPermanentBounceCooldown
		}
	} else {
		memory.AckCount++
		cooldown = inboxAcknowledgedCooldown
	}
	if cooldown > 0 {
		until := record.ReceivedAt.Add(cooldown)
		if memory.CooldownUntil == nil || until.After(*memory.CooldownUntil) {
			memory.CooldownUntil = &until
		}
	}
	if _, err := d.UpsertContactEndpointMemory(ctx, memory); err != nil {
		return fmt.Errorf("update endpoint memory: %w", err)
	}
	return nil
}
