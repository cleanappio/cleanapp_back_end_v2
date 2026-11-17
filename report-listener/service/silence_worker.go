package service

import (
	"context"
	"log"
	"time"

	"report-listener/database"
)

// recordSilence marks notifications that got nothing back within the waiting
// period as `no_response` (phase 5 of
// docs/architecture/notification-outcomes.md). It runs right after a
// successful inbox pass and never otherwise: without a current read of the
// inbox, "nothing came back" cannot be known.
func (s *Service) recordSilence(ctx context.Context, now time.Time) {
	cfg := s.config
	if !cfg.EscalationEnabled || cfg.EscalationWaitWorkingDays <= 0 {
		return
	}
	sentBefore := database.AddWorkingDays(now, -cfg.EscalationWaitWorkingDays)
	candidates, err := s.db.SilenceCandidates(ctx, sentBefore, cfg.InboxLookback)
	if err != nil {
		log.Printf("warn: silence worker: %v", err)
		return
	}
	notifications, reports := 0, int64(0)
	for _, id := range candidates {
		if ctx.Err() != nil {
			return
		}
		written, err := s.db.RecordNoResponse(ctx, id, cfg.EscalationWaitWorkingDays, now)
		if err != nil {
			// The notification ID is not a secret; the address is not logged.
			log.Printf("warn: silence worker: notification %s: %v", id, err)
		}
		if written > 0 {
			notifications++
			reports += written
		}
	}
	if notifications > 0 {
		log.Printf("silence worker: no response after %d working days for %d notifications (%d reports)", cfg.EscalationWaitWorkingDays, notifications, reports)
	}
}
