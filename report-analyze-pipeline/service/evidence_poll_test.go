package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
)

type fakeEvidenceTimer struct {
	delay   time.Duration
	channel chan time.Time
	mu      sync.Mutex
	stopped bool
}

func (t *fakeEvidenceTimer) C() <-chan time.Time { return t.channel }
func (t *fakeEvidenceTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	return true
}
func (t *fakeEvidenceTimer) fire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.stopped {
		t.channel <- time.Time{}
	}
}

type fakeEvidenceClock struct{ timers chan *fakeEvidenceTimer }

func (c *fakeEvidenceClock) newTimer(delay time.Duration) evidencePollTimer {
	timer := &fakeEvidenceTimer{delay: delay, channel: make(chan time.Time, 1)}
	c.timers <- timer
	return timer
}

func nextEvidenceTimer(t *testing.T, clock *fakeEvidenceClock, delay time.Duration) *fakeEvidenceTimer {
	t.Helper()
	select {
	case timer := <-clock.timers:
		if timer.delay != delay {
			t.Fatalf("poll delay=%s want=%s", timer.delay, delay)
		}
		return timer
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not schedule the next virtual timer")
		return nil
	}
}

func awaitEvidencePollingShutdown(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller or shutdown watcher did not exit")
	}
}

func TestEvidencePollingBacksOffEmptyAndFailedScansThenResumesAfterWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeEvidenceClock{timers: make(chan *fakeEvidenceTimer, 8)}
	outcomes := []evidenceBatchState{evidenceBatchEmpty, evidenceBatchQueryFailed, evidenceBatchFound, evidenceBatchEmpty}
	calls := make(chan evidenceBatchState, len(outcomes))
	done := make(chan struct{})
	go func() {
		defer close(done)
		index := 0
		runEvidencePolling(ctx, clock.newTimer, func(context.Context) evidenceBatchState {
			outcome := outcomes[index]
			index++
			calls <- outcome
			return outcome
		})
	}()
	timer := nextEvidenceTimer(t, clock, 5*time.Second)
	for i, delay := range []time.Duration{60 * time.Second, 60 * time.Second, 5 * time.Second, 60 * time.Second} {
		timer.fire()
		select {
		case state := <-calls:
			if state != outcomes[i] {
				t.Fatal("wrong virtual scan outcome")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("virtual timer did not run a scan")
		}
		timer = nextEvidenceTimer(t, clock, delay)
	}
	cancel()
	awaitEvidencePollingShutdown(t, done)
	if !timer.stopped {
		t.Fatal("idle shutdown left a live timer")
	}
}

func TestEvidencePollingCanceledWaitDoesNotRunAnotherScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeEvidenceClock{timers: make(chan *fakeEvidenceTimer, 2)}
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEvidencePolling(ctx, clock.newTimer, func(context.Context) evidenceBatchState {
			calls++
			return evidenceBatchFound
		})
	}()
	timer := nextEvidenceTimer(t, clock, 5*time.Second)
	cancel()
	awaitEvidencePollingShutdown(t, done)
	timer.fire()
	if calls != 0 || !timer.stopped {
		t.Fatal("shutdown allowed an extra scan or retained a live timer")
	}
}

type blockingEvidenceQueryStore struct {
	*analysisStore
	queried chan context.Context
}

func (s *blockingEvidenceQueryStore) GetPendingEvidenceReports(ctx context.Context, _ int) ([]database.PendingEvidence, error) {
	s.queried <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEvidenceWorkerShutdownCancelsScanAndPreservesPrimaryInterval(t *testing.T) {
	store := &blockingEvidenceQueryStore{analysisStore: &analysisStore{}, queried: make(chan context.Context, 1)}
	client := &analysisClient{}
	publisher := &recordingPublisher{}
	svc := &Service{config: &config.Config{AnalysisInterval: 500 * time.Millisecond, EvidenceWorkerBatchSize: 10}, db: store, llmClient: client, publisher: publisher, stopChan: make(chan bool)}
	clock := &fakeEvidenceClock{timers: make(chan *fakeEvidenceTimer, 2)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.processPendingEvidenceWithClock(clock.newTimer)
	}()
	nextEvidenceTimer(t, clock, 5*time.Second).fire()
	var queryContext context.Context
	select {
	case queryContext = <-store.queried:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start the virtual scan")
	}
	close(svc.stopChan)
	awaitEvidencePollingShutdown(t, done)
	if queryContext.Err() != context.Canceled || svc.config.AnalysisInterval != 500*time.Millisecond {
		t.Fatal("shutdown failed to cancel scan or changed primary analysis interval")
	}
	if client.calls != 0 || len(store.evidenceWrites) != 0 || len(publisher.events) != 0 {
		t.Fatal("shutdown performed classification, persistence, or routing")
	}
}
