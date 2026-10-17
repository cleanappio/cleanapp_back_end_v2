package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type cohortMaintenanceFixture struct {
	c        *evidenceCohortCache
	ticks    chan time.Time
	passDone chan struct{}
	clock    atomic.Int64
}

func newCohortMaintenanceFixture(t *testing.T, maxKeys int, load func(context.Context, EvidenceFilter, time.Duration) (evidenceCohort, error)) *cohortMaintenanceFixture {
	t.Helper()
	h := &cohortMaintenanceFixture{ticks: make(chan time.Time), passDone: make(chan struct{}, 1)}
	h.clock.Store(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC).UnixNano())
	d := &Database{}
	cfg := cohortTestConfig()
	cfg.maxKeys = maxKeys
	d.evidenceCohorts.configure(cfg)
	d.evidenceCohorts.maintenanceTicks = h.ticks
	d.evidenceCohorts.maintenancePassDone = h.passDone
	d.evidenceCohorts.now = func() time.Time { return time.Unix(0, h.clock.Load()).UTC() }
	d.evidenceCohorts.load = load
	h.c = cohortTestCache(t, d)
	return h
}

func (h *cohortMaintenanceFixture) advance(delta time.Duration) {
	h.clock.Add(int64(delta))
}

// Wait for the entire maintenance pass before waiting on its scheduled loads.
// The next fake-clock advance cannot leak into an unacknowledged extra tick.
func (h *cohortMaintenanceFixture) tick(t *testing.T) {
	t.Helper()
	select {
	case h.ticks <- h.c.now():
	case <-time.After(5 * time.Second):
		t.Fatal("cache maintenance did not consume a tick")
	}
	select {
	case <-h.passDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cache maintenance did not finish its pass")
	}
	h.c.inflight.Wait()
}

func maintenanceFilter(brand string) EvidenceFilter {
	f, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: brand})
	return f
}

func TestEvidenceCohortMaintenanceRefreshesStaggeredIdleKeys(t *testing.T) {
	var h *cohortMaintenanceFixture
	var calls atomic.Int32
	h = newCohortMaintenanceFixture(t, 2, func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		calls.Add(1)
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: h.c.now()}, nil
	})
	microsoft, google := maintenanceFilter("microsoft"), maintenanceFilter("google")
	h.c.lookup(microsoft)
	h.c.inflight.Wait()
	h.advance(6 * time.Second)
	h.c.lookup(google)
	h.c.inflight.Wait()
	h.advance(54 * time.Second)
	h.tick(t) // Microsoft is due; Google's snapshot is only 54 seconds old.
	if calls.Load() != 3 {
		t.Fatalf("maintenance did not refresh only the first due idle cohort: %d", calls.Load())
	}
	h.advance(6 * time.Second)
	h.tick(t)
	if calls.Load() != 4 {
		t.Fatal("staggered Google cohort waited for another full interval")
	}
	// No lookups between ticks: keep both cohorts warm past the original
	// 120-second expiry, then require the first returning visitor to hit memory.
	h.advance(54 * time.Second)
	h.tick(t)
	h.advance(6 * time.Second)
	h.tick(t)
	for _, f := range []EvidenceFilter{microsoft, google} {
		cohort, held := h.c.lookup(f)
		if !held || h.c.now().Sub(cohort.snapshotAt) >= h.c.config.interval+h.c.config.budget {
			t.Fatalf("idle brand %s fell through to the direct reader", f.BrandName)
		}
	}
	if calls.Load() != 6 {
		t.Fatal("returning visitors caused an extra full-cohort load")
	}
}

func TestEvidenceCohortMaintenanceEvictionHasNoOrphanLoadsOrRecencyChanges(t *testing.T) {
	var h *cohortMaintenanceFixture
	loads := make(chan string, 8)
	h = newCohortMaintenanceFixture(t, 2, func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		loads <- f.BrandName
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: h.c.now()}, nil
	})
	alpha, beta, gamma := maintenanceFilter("alpha"), maintenanceFilter("beta"), maintenanceFilter("gamma")
	for _, f := range []EvidenceFilter{alpha, beta} {
		h.c.lookup(f)
		h.c.inflight.Wait()
		<-loads
	}
	h.advance(time.Minute)
	h.tick(t)
	<-loads
	<-loads
	h.c.lookup(gamma)
	h.c.inflight.Wait()
	if <-loads != "gamma" {
		t.Fatal("newly requested key was not loaded")
	}
	h.c.mu.Lock()
	_, alphaHeld := h.c.entries[evidenceCohortKey(alpha)]
	h.c.mu.Unlock()
	if alphaHeld {
		t.Fatal("maintenance changed LRU request recency")
	}
	h.advance(time.Minute)
	h.tick(t)
	for i := 0; i < 2; i++ {
		if brand := <-loads; brand == "alpha" {
			t.Fatal("evicted key retained an orphan maintenance load")
		}
	}
	if len(loads) != 0 {
		t.Fatal("maintenance admitted an unrequested key")
	}
}

func TestEvidenceCohortMaintenancePreservesBackoffExpiryAndStop(t *testing.T) {
	var h *cohortMaintenanceFixture
	var calls atomic.Int32
	h = newCohortMaintenanceFixture(t, 1, func(_ context.Context, f EvidenceFilter, _ time.Duration) (evidenceCohort, error) {
		if calls.Add(1) > 1 {
			return evidenceCohort{}, errors.New("read failed")
		}
		return evidenceCohort{key: evidenceCohortKey(f), snapshotAt: h.c.now()}, nil
	})
	f := maintenanceFilter("google")
	h.c.lookup(f)
	h.c.inflight.Wait()
	h.advance(time.Minute)
	h.tick(t) // failed refresh, retry after two intervals
	h.advance(time.Minute)
	h.tick(t)
	if calls.Load() != 2 {
		t.Fatal("idle upkeep bypassed failed-refresh backoff")
	}
	if _, usable := h.c.lookup(f); usable {
		t.Fatal("maintenance failure extended the hard snapshot expiry")
	}
	h.advance(time.Minute)
	h.tick(t)
	if calls.Load() != 3 {
		t.Fatal("idle upkeep did not retry after backoff")
	}
	h.c.stop()
	select {
	case <-h.c.maintenanceDone:
	default:
		t.Fatal("Stop returned before the maintenance loop exited")
	}
	h.advance(time.Hour)
	h.c.lookup(f)
	h.c.inflight.Wait()
	if calls.Load() != 3 {
		t.Fatal("stopped maintenance scheduled another load")
	}
}
