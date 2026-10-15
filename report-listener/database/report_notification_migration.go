package database

import (
	"context"
	"database/sql"
)

func ensureReportEmailDeliveryEvents(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS report_email_delivery_events (
   id BIGINT AUTO_INCREMENT PRIMARY KEY,
   seq INT NOT NULL,
   recipient_email VARCHAR(320) NOT NULL,
   delivery_status VARCHAR(32) NOT NULL DEFAULT 'sent',
   delivery_source VARCHAR(64) NOT NULL,
   provider VARCHAR(64) NOT NULL DEFAULT '',
   provider_message_id VARCHAR(255) NULL,
   receipt_key CHAR(64) NULL,
   sent_at TIMESTAMP NULL,
   legacy_delivery_id BIGINT NULL,
   is_legacy BOOLEAN NOT NULL DEFAULT FALSE,
   created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
   UNIQUE KEY uq_report_delivery_event_legacy (legacy_delivery_id, sent_at, delivery_source),
   UNIQUE KEY uq_report_delivery_event_receipt (receipt_key),
   INDEX idx_report_delivery_event_seq (seq, sent_at, id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return err
}
