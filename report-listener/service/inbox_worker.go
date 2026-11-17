package service

import (
	"context"
	"log"
	"time"

	"report-listener/inbox"
)

// startInboxWorker reads replies to notification emails (phase 1b of
// docs/architecture/notification-outcomes.md). It is off unless
// INBOX_ENABLED is set. A missing secret, missing credentials or an
// unreadable key leaves it off and says why once: reading the inbox must
// never stop the service from starting.
func (s *Service) startInboxWorker(ctx context.Context) {
	cfg := s.config
	if !cfg.InboxEnabled {
		return
	}
	if len(cfg.ReplyTokenSecret) < 16 {
		log.Printf("error: INBOX_ENABLED is set but REPLY_TOKEN_SECRET is missing or shorter than 16 bytes; the inbox worker is off")
		return
	}
	if cfg.InboxCredentialsFile == "" || cfg.InboxUser == "" {
		log.Printf("error: INBOX_ENABLED is set but INBOX_CREDENTIALS_FILE or INBOX_USER is empty; the inbox worker is off")
		return
	}
	mailbox, err := inbox.NewGmail(ctx, cfg.InboxCredentialsFile, cfg.InboxUser)
	if err != nil {
		log.Printf("error: inbox worker is off: %v", err)
		return
	}
	interval := cfg.InboxPollInterval
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Printf("inbox worker started: mailbox=%s every=%s lookback=%s", cfg.InboxUser, interval, cfg.InboxLookback)
		var lastSilencePass time.Time
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			pollCtx, cancel := context.WithTimeout(ctx, interval)
			counts, err := inbox.Poll(pollCtx, mailbox, s.db, []byte(cfg.ReplyTokenSecret), time.Now().Add(-cfg.InboxLookback))
			cancel()
			if len(counts) > 0 {
				log.Printf("inbox worker recorded: %v", counts)
			}
			if err != nil && ctx.Err() == nil {
				log.Printf("warn: inbox worker: %v", err)
			}
			// Only a complete read of the inbox can show that nothing came back.
			// The waiting period is days, so one silence pass an hour is enough.
			if now := time.Now().UTC(); err == nil && now.Sub(lastSilencePass) >= time.Hour {
				silenceCtx, cancelSilence := context.WithTimeout(ctx, interval)
				s.recordSilence(silenceCtx, now)
				cancelSilence()
				lastSilencePass = now
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
