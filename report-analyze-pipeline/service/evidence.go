package service

import (
	"cleanapp-common/evidence"
	"context"
	"log"
	"time"
)

const (
	evidenceActivePollInterval = 5 * time.Second
	evidenceIdlePollInterval   = 60 * time.Second
)

type evidenceBatchState uint8

const (
	evidenceBatchEmpty evidenceBatchState = iota
	evidenceBatchFound
	evidenceBatchQueryFailed
	evidenceBatchPartial
)

type evidencePollTimer interface {
	C() <-chan time.Time
	Stop() bool
}
type realEvidenceTimer struct{ *time.Timer }

func (t realEvidenceTimer) C() <-chan time.Time { return t.Timer.C }

// A fresh timer starts after each batch completes. Idle/failed scans therefore
// cannot accumulate ticker events and immediately repeat a costly query.
func runEvidencePolling(ctx context.Context, newTimer func(time.Duration) evidencePollTimer, batch func(context.Context) evidenceBatchState) {
	delay := evidenceActivePollInterval
	for ctx.Err() == nil {
		timer := newTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
		state := batch(ctx)
		delay = evidenceIdlePollInterval
		if state == evidenceBatchFound || state == evidenceBatchPartial {
			delay = evidenceActivePollInterval
		}
	}
}

func (s *Service) processPendingEvidence() {
	s.processPendingEvidenceWithClock(func(delay time.Duration) evidencePollTimer {
		return realEvidenceTimer{time.NewTimer(delay)}
	})
}

func (s *Service) processPendingEvidenceWithClock(newTimer func(time.Duration) evidencePollTimer) {
	ctx, cancel := context.WithCancel(context.Background())
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-s.stopChan:
			cancel()
		case <-ctx.Done():
		}
	}()
	defer func() { cancel(); <-watcherDone }()
	runEvidencePolling(ctx, newTimer, s.processEvidenceBatchWithContext)
}

func (s *Service) processEvidenceBatch() evidenceBatchState {
	return s.processEvidenceBatchWithContext(context.Background())
}

func (s *Service) processEvidenceBatchWithContext(ctx context.Context) evidenceBatchState {
	// Bounded, sequential work avoids duplicate bursts of provider calls. The
	// primary analyzer persists once per seq, so missing/failed evidence and
	// bypassed imports enter without redoing their existing normal analyses.
	batchSize := s.config.EvidenceWorkerBatchSize
	if batchSize < 1 {
		batchSize = 10
	}
	if batchSize > 50 {
		batchSize = 50
	}
	// Limit only candidate selection; provider and persistence work keep their
	// independent contexts after this short query deadline is released.
	queryCtx, cancelQuery := context.WithTimeout(ctx, 10*time.Second)
	batch, err := s.db.ScanPendingEvidenceReports(queryCtx, batchSize, s.evidenceCursor)
	cancelQuery()
	if err != nil {
		log.Print("Evidence worker: pending query failed")
		return evidenceBatchQueryFailed
	}
	s.evidenceCursor = batch.NextCursor
	if len(batch.Items) == 0 {
		if batch.MoreToScan {
			return evidenceBatchPartial
		}
		return evidenceBatchEmpty
	}
	classifier, _ := s.llmClient.(evidence.Classifier)
	for _, item := range batch.Items {
		select {
		case <-s.stopChan:
			return evidenceBatchFound
		case <-ctx.Done():
			return evidenceBatchFound
		default:
		}
		original, err := evidence.LoadOriginal(ctx, s.db.GetDB(), item.Seq)
		fields := evidence.Unknown("Stored original could not be resolved.")
		if err == nil {
			fields, err = evidence.Classify(classifier, original, item.Brand)
		}
		if err != nil {
			log.Printf("Evidence worker seq=%d: resolution/classification failed", item.Seq)
			continue
		}
		if err := s.db.SaveEvidence(ctx, item.Seq, fields, evidence.Version); err != nil {
			log.Printf("Evidence worker seq=%d: save failed; retaining retry eligibility", item.Seq)
		}
	}
	return evidenceBatchFound
}
