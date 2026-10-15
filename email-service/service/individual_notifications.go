package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"email-service/email"
	"email-service/models"

	"github.com/apex/log"
)

type individualNotificationOutcome struct {
	Eligible        int
	Accepted        int
	AlreadyAccepted int
	Held            int
	Failed          int
	Complete        bool
	RetryAt         *time.Time
	RetryReason     string
}

const retryReasonIndividualPublicPolicy = "report_not_public_or_valid"

// sendIndividualReportNotifications persists each acceptance before attempting
// another recipient. The old batch API is called with exactly one address, so
// a later failure cannot erase an earlier successful send. A complete outcome
// requires a recorded acceptance for every distinct requested recipient.
// An empty throttleKey preserves area-contact policy; inferred contacts supply
// their brand key. This helper never creates a processed marker.
func (s *EmailService) sendIndividualReportNotifications(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, recipients []string, mapImage []byte, source, throttleKey string) (individualNotificationOutcome, error) {
	out := individualNotificationOutcome{}
	unique := make([]string, 0, len(recipients))
	seen := make(map[string]bool)
	for _, recipient := range recipients {
		recipient = strings.ToLower(strings.TrimSpace(recipient))
		if recipient != "" && !seen[recipient] {
			seen[recipient] = true
			unique = append(unique, recipient)
		}
	}
	out.Eligible = len(unique)
	if s.config.DryRun {
		out.Held = len(unique)
		return out, nil
	}
	finishIncomplete := func(reason string, at time.Time, cause error) (individualNotificationOutcome, error) {
		out.RetryReason, out.RetryAt = reason, &at
		if cause == nil {
			cause = fmt.Errorf("individual report notification is incomplete: accepted=%d prior=%d held=%d failed=%d", out.Accepted, out.AlreadyAccepted, out.Held, out.Failed)
		}
		if err := s.scheduleReportRetry(ctx, report.Seq, reason, at); err != nil {
			return out, fmt.Errorf("%w; retry scheduling failed: %v", cause, err)
		}
		return out, cause
	}
	if len(unique) == 0 {
		return finishIncomplete(retryReasonNoValidEmails, time.Now().Add(6*time.Hour), nil)
	}
	throttleKey = strings.ToLower(strings.TrimSpace(throttleKey))
	preflighted := false
	imageLoaded := report.Image != nil
	var earliestThrottle *time.Time
	var sendFailure error
	for _, recipient := range unique {
		prior, err := s.alreadyNotifiedAllReports(ctx, []int64{report.Seq}, recipient)
		if err != nil {
			out.Held++
			return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("check prior report notification: %w", err))
		}
		if prior {
			out.AlreadyAccepted++
			continue
		}
		optedOut, err := s.isEmailOptedOut(ctx, recipient)
		if err != nil {
			out.Held++
			return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("check recipient opt-out: %w", err))
		}
		if optedOut {
			out.Held++
			continue
		}
		if throttleKey != "" {
			throttled, next, err := s.getThrottleDecision(ctx, throttleKey, recipient)
			if err != nil {
				out.Held++
				return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("check individual notification throttle: %w", err))
			}
			if throttled {
				out.Held++
				if next != nil && (earliestThrottle == nil || next.Before(*earliestThrottle)) {
					earliestThrottle = next
				}
				continue
			}
		}
		if !preflighted {
			var publishable bool
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reports r
				JOIN report_analysis ra ON ra.seq=r.seq AND ra.language='en'
				LEFT JOIN report_raw rr ON rr.report_seq=r.seq
				WHERE r.seq=? AND ra.is_valid=TRUE
				AND (rr.visibility IS NULL OR rr.visibility='public') AND `+notificationPublicStateSQL("r")+`)`, report.Seq).Scan(&publishable); err != nil {
				out.Held++
				return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("check individual report public policy: %w", err))
			}
			if !publishable {
				out.Held++
				return finishIncomplete(retryReasonIndividualPublicPolicy, time.Now().Add(24*time.Hour), fmt.Errorf("report is not eligible for public notification"))
			}
			if err := s.preflightNotificationReceipts(ctx); err != nil {
				out.Held++
				return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("individual receipt preflight: %w", err))
			}
			preflighted = true
		}
		if !imageLoaded {
			imageLoaded = true
			if image, err := s.getReportImage(ctx, report.Seq); err == nil {
				report.Image = image
			} else {
				log.Warnf("Individual notification report image unavailable: seq=%d error=%v", report.Seq, err)
			}
		}
		if err := s.email.SendEmailsWithAnalysis([]string{recipient}, report.Image, mapImage, analysis); err != nil {
			out.Failed++
			if sendFailure == nil {
				sendFailure = err
			}
			continue
		}
		out.Accepted++
		accepted := email.CustomEmailSendResult{Email: recipient, Status: "sent", Provider: s.email.Provider(), SentAt: time.Now().UTC()}
		if err := s.recordReportEmailDeliveryResult(ctx, report.Seq, recipient, source, accepted); err != nil {
			recovery, _ := json.Marshal(map[string]any{"seq": report.Seq, "recipient": recipient, "source": source, "provider": accepted.Provider, "sent_at": accepted.SentAt, "error": err.Error()})
			log.Errorf("notification_receipt_recovery %s", recovery)
			return finishIncomplete(retryReasonReceiptPending, time.Now().Add(24*time.Hour), fmt.Errorf("accepted individual notification receipt failed; report quarantined: %w", err))
		}
		if throttleKey != "" {
			if err := s.recordBrandEmailSent(ctx, throttleKey, recipient); err != nil {
				return finishIncomplete(retryReasonThrottleError, time.Now().Add(30*time.Minute), fmt.Errorf("accepted individual notification throttle persistence: %w", err))
			}
		}
		if err := s.recordEmailSent(ctx, recipient); err != nil {
			log.Warnf("Individual recipient history persistence failed after recorded acceptance: seq=%d error=%v", report.Seq, err)
		}
	}
	if out.Failed > 0 {
		return finishIncomplete(retryReasonAggregateFailure, time.Now().Add(30*time.Minute), fmt.Errorf("individual notification send failed: %w", sendFailure))
	}
	if out.Held > 0 {
		if earliestThrottle != nil {
			return finishIncomplete(retryReasonAllThrottled, *earliestThrottle, nil)
		}
		return finishIncomplete(retryReasonNoValidEmails, time.Now().Add(6*time.Hour), nil)
	}
	out.Complete = out.Accepted+out.AlreadyAccepted == out.Eligible
	return out, nil
}
