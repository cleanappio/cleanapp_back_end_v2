package service

import (
	"context"
	"errors"
	"testing"

	"cleanapp-common/effectpolicy"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
)

func TestHistoricalReportSkipsAnalysisBeforeProviderOrWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		blocked   bool
		policyErr error
	}{
		{name: "historical cohort", blocked: true},
		{name: "unavailable policy", policyErr: errors.New("policy unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &analysisStore{policy: map[effectpolicy.Effect]bool{effectpolicy.Analysis: tc.blocked}, policyErr: tc.policyErr}
			publisher := &recordingPublisher{}
			// A nil provider is intentional: the guard must precede all model,
			// hydration, contact, persistence and publication work.
			svc := &Service{db: store, publisher: publisher}
			err := svc.AnalyzeReport(&database.Report{Seq: 71})
			if (err != nil) != (tc.policyErr != nil) {
				t.Fatalf("policy result: %v", err)
			}
			if len(store.analyses) != 0 || len(store.evidenceWrites) != 0 || len(publisher.events) != 0 || store.publishedMarkers != 0 {
				t.Fatal("suppressed report produced analysis or outbound work")
			}
		})
	}
}

func TestAnalysedEventChecksEveryOutboundEffectAtPublication(t *testing.T) {
	for _, effect := range []effectpolicy.Effect{effectpolicy.Notifications, effectpolicy.Routing, effectpolicy.Rewards} {
		t.Run(string(effect), func(t *testing.T) {
			store := &analysisStore{policy: map[effectpolicy.Effect]bool{effect: true}}
			publisher := &recordingPublisher{}
			svc := &Service{db: store, publisher: publisher}
			if err := svc.publishAnalyzedReport(&database.Report{Seq: 71}, nil); err != nil {
				t.Fatal(err)
			}
			if len(publisher.events) != 0 || store.publishedMarkers != 0 {
				t.Fatal("muted analysed event was published or marked as delivered")
			}
		})
	}
	store := &analysisStore{policyErr: errors.New("policy unavailable")}
	publisher := &recordingPublisher{}
	if err := (&Service{db: store, publisher: publisher}).publishAnalyzedReport(&database.Report{Seq: 71}, nil); err == nil || len(publisher.events) != 0 {
		t.Fatal("publication did not fail closed")
	}
}

func TestEvidenceWorkerRechecksHistoricalPolicyBeforeProviderAndSave(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		store := &analysisStore{report: &database.Report{Seq: 71}, policy: map[effectpolicy.Effect]bool{effectpolicy.Analysis: true}}
		if unavailable {
			store.policyErr = errors.New("policy unavailable")
		}
		svc := &Service{db: store, config: &config.Config{EvidenceWorkerBatchSize: 1}, stopChan: make(chan bool)}
		svc.processEvidenceBatchWithContext(context.Background())
		if len(store.evidenceWrites) != 0 {
			t.Fatal("evidence worker wrote a suppressed historical report")
		}
	}
}
