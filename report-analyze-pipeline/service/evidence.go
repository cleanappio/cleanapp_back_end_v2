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
	// primary analyzer already persists version1, so only bypassed imports enter.
	ctx := context.Background()
	batchSize := s.config.EvidenceWorkerBatchSize
	if batchSize < 1 {
		batchSize = 10
	}
	if batchSize > 50 {
		batchSize = 50
	}
	pending, err := s.db.GetPendingEvidenceReports(ctx, batchSize)
	if err != nil {
		log.Printf("Evidence worker: %v", err)
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
		if err := s.db.SaveEvidence(ctx, item.Seq, fields); err != nil {
			log.Printf("Evidence worker seq=%d: save failed: %v", item.Seq, err)
		}
	}
}
