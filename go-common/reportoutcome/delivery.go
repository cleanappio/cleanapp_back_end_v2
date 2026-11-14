package reportoutcome

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

func (s *Store) delivery(ctx context.Context, seq int, outcome *Outcome, eventPending bool) error {
	if len(outcome.Delivery) != 2 {
		outcome.Delivery = []Delivery{{Channel: "email", Status: "not_requested"}, {Channel: "blockscan", Status: "not_requested"}}
	}
	var emailCount int
	var emailAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MAX(sent_at) FROM (
 SELECT sent_at FROM report_email_delivery_events WHERE seq=? AND delivery_status='sent'
 UNION ALL SELECT old.sent_at FROM report_email_deliveries old WHERE old.seq=? AND old.delivery_status='sent'
 AND NOT EXISTS(SELECT 1 FROM report_email_delivery_events e WHERE e.seq=old.seq AND e.delivery_status='sent'
 AND e.recipient_email=old.recipient_email AND e.delivery_source=old.delivery_source AND e.sent_at<=>old.sent_at)
 ) receipts`, seq, seq).Scan(&emailCount, &emailAt)
	if err != nil {
		return err
	}
	if emailCount > 0 {
		outcome.Delivery[0] = Delivery{Channel: "email", Status: "uncertain", EvidenceCount: emailCount, ReasonCode: "provider_receipt_incomplete"}
		if emailAt.Valid {
			outcome.Delivery[0].Status, outcome.Delivery[0].ReasonCode = "sent", "recorded_provider_send"
			value := emailAt.Time.UTC()
			outcome.Delivery[0].LastSentAt = &value
		}
	} else if outcome.Delivery[0].Status == "pending" {
		var reason string
		var next sql.NullTime
		err = s.db.QueryRowContext(ctx, `SELECT reason,next_attempt_at FROM email_report_retry WHERE seq=?`, seq).Scan(&reason, &next)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		switch reason {
		case "":
			var assessed bool
			if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sent_reports_emails WHERE seq=?)`, seq).Scan(&assessed); err != nil {
				return err
			}
			if assessed {
				outcome.Delivery[0].Status, outcome.Delivery[0].ReasonCode = "held", "no_recorded_delivery"
			} else {
				outcome.Delivery[0].ReasonCode = "routing_evaluation_pending"
			}
		case "no_valid_emails", "await_contact_discovery":
			outcome.Delivery[0].Status = "held"
			outcome.Delivery[0].ReasonCode = "no_verified_recipient"
		case "notification_receipt_pending":
			outcome.Delivery[0].Status = "uncertain"
			outcome.Delivery[0].ReasonCode = "provider_receipt_pending"
		case "daily_limit", "all_throttled":
			outcome.Delivery[0].ReasonCode = "rate_limited"
		case "aggregate_send_failed", "throttle_check_error":
			outcome.Delivery[0].Status = "failed"
			outcome.Delivery[0].ReasonCode = "notification_send_failed"
			if next.Valid {
				outcome.Delivery[0].Status = "pending"
				outcome.Delivery[0].ReasonCode = "provider_retry_scheduled"
			}
		}
	}
	var state, messageID string
	var sentAt sql.NullTime
	var nextAttempt sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT status,COALESCE(blockscan_message_id,''),sent_at,next_attempt_at FROM epc_outbox WHERE event_key=? LIMIT 1`, "prod:report:"+strconv.Itoa(seq)).Scan(&state, &messageID, &sentAt, &nextAttempt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) && outcome.Delivery[1].Status == "pending" {
		var eligible bool
		if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_analysis WHERE seq=? AND is_valid=TRUE AND brand_name IS NOT NULL AND TRIM(brand_name)<>'')`, seq).Scan(&eligible); err != nil {
			return err
		}
		if eligible {
			outcome.Delivery[1].ReasonCode = "routing_evaluation_pending"
		} else {
			outcome.Delivery[1].Status, outcome.Delivery[1].ReasonCode = "held", "no_eligible_blockscan_subject"
		}
	}
	if err == nil {
		switch state {
		case "sent":
			if messageID != "" && sentAt.Valid {
				outcome.Delivery[1] = Delivery{Channel: "blockscan", Status: "sent", EvidenceCount: 1, ReasonCode: "recorded_provider_send"}
				if sentAt.Valid {
					value := sentAt.Time.UTC()
					outcome.Delivery[1].LastSentAt = &value
				}
			} else {
				outcome.Delivery[1] = Delivery{Channel: "blockscan", Status: "uncertain", ReasonCode: "provider_receipt_incomplete"}
			}
		case "held":
			outcome.Delivery[1].Status = "held"
			outcome.Delivery[1].ReasonCode = "existing_conversation_required"
		case "uncertain", "sending":
			outcome.Delivery[1].Status = "uncertain"
			outcome.Delivery[1].ReasonCode = "provider_outcome_uncertain"
		case "error":
			outcome.Delivery[1].Status = "failed"
			outcome.Delivery[1].ReasonCode = "notification_send_failed"
			if nextAttempt.Valid {
				outcome.Delivery[1].Status = "pending"
				outcome.Delivery[1].ReasonCode = "provider_retry_scheduled"
			}
		case "pending":
			outcome.Delivery[1].Status = "pending"
			outcome.Delivery[1].ReasonCode = "provider_send_pending"
		}
	}
	if outcome.Processing.Status == "analyzed" && outcome.Publication.Status == "public" {
		complete := true
		for _, delivery := range outcome.Delivery {
			if delivery.Status == "pending" {
				complete = false
			}
		}
		outcome.Final = complete
		if complete {
			outcome.NextAction = NextAction{Type: "none"}
		}
	}
	if eventPending {
		outcome.Final = false
		outcome.NextAction = NextAction{Type: "wait"}
	}
	return nil
}

func publicationDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(30*(1<<uint(attempt-1))) * time.Second
}
