package service

import (
	"cleanapp-common/evidence"
	"context"
	"log"
	"time"
)

func (s *Service) processPendingEvidence() {
	interval := s.config.AnalysisInterval
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.processEvidenceBatch()
		}
	}
}

func (s *Service) processEvidenceBatch() {
	// Bounded, sequential work avoids duplicate bursts of provider calls. The
	// primary analyzer persists once per seq, so missing/failed evidence and
	// bypassed imports enter without redoing their existing normal analyses.
	ctx := context.Background()
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
	pending, err := s.db.GetPendingEvidenceReports(queryCtx, batchSize)
	cancelQuery()
	if err != nil {
		log.Print("Evidence worker: pending query failed")
		return
	}
	classifier, _ := s.llmClient.(evidence.Classifier)
	for _, item := range pending {
		select {
		case <-s.stopChan:
			return
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
}
