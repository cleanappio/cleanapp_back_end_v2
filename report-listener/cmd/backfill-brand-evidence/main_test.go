package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func validConfig() config {
	return config{Mode: "audit", Brand: "microsoft", ThroughSeq: 1000, BatchSize: 2, Timeout: time.Second}
}

func TestExplicitBoundedScopeRequired(t *testing.T) {
	for _, change := range []func(*config){
		func(c *config) { c.Brand = "" },
		func(c *config) { c.ThroughSeq = 0 },
		func(c *config) { c.FromSeq = 1001 },
		func(c *config) { c.AfterSeq = 1001 },
		func(c *config) { c.BatchSize = 201 },
		func(c *config) { c.BatchSize = 0 },
		func(c *config) { c.Mode = "migrate" },
		func(c *config) { c.Timeout = 61 * time.Second },
		func(c *config) { c.Pause = -time.Second },
	} {
		c := validConfig()
		change(&c)
		if c.validate() == nil {
			t.Fatalf("accepted unsafe scope: %+v", c)
		}
	}
}

type fakeStore struct {
	ids      []int
	warm     map[int]bool
	copied   [][]int
	audited  []int
	failCopy bool
}

func (s *fakeStore) selectBatch(_ context.Context, c config, after int) ([]int, error) {
	var ids []int
	for _, seq := range s.ids {
		if seq > after && seq >= c.FromSeq && seq <= c.ThroughSeq && len(ids) < c.BatchSize {
			ids = append(ids, seq)
		}
	}
	return ids, nil
}

func (s *fakeStore) backfill(_ context.Context, ids []int) error {
	s.copied = append(s.copied, append([]int(nil), ids...))
	if s.failCopy {
		return errors.New("interrupted after first projection")
	}
	for _, seq := range ids {
		s.warm[seq] = true
	}
	return nil
}

func (s *fakeStore) audit(_ context.Context, ids []int) (coverage, error) {
	s.audited = append(s.audited, ids...)
	c := coverage{Reports: int64(len(ids))}
	for _, seq := range ids {
		if !s.warm[seq] {
			c.MissingReports++
		}
	}
	return c, nil
}

func TestHistoricalProjectionGapFailsAuditThenRepairsEveryBatch(t *testing.T) {
	c := validConfig()
	s := &fakeStore{ids: []int{1, 7, 30, 80, 1001}, warm: map[int]bool{}}
	var events []event
	emit := func(e event) { events = append(events, e) }
	if execute(context.Background(), c, s, emit) == nil {
		t.Fatal("historical missing projection was reported complete")
	}
	if len(s.copied) != 0 {
		t.Fatal("default audit mutated a projection")
	}
	c.Mode = "apply"
	s.audited = nil
	if err := execute(context.Background(), c, s, emit); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.copied, [][]int{{1, 7}, {30, 80}}) || !reflect.DeepEqual(s.audited, []int{1, 7, 30, 80}) || s.warm[1001] {
		t.Fatalf("truncated or exceeded approved cohort: copied=%v audited=%v", s.copied, s.audited)
	}
	if events[len(events)-1].Stage != "complete" {
		t.Fatal("successful full audit did not mark complete")
	}
}

func TestResumeStillAuditsReportsBeforeCheckpoint(t *testing.T) {
	c := validConfig()
	c.Mode, c.AfterSeq = "apply", 7
	s := &fakeStore{ids: []int{1, 7, 30, 80}, warm: map[int]bool{1: true, 7: true}}
	if err := execute(context.Background(), c, s, func(event) {}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.copied, [][]int{{30, 80}}) || !reflect.DeepEqual(s.audited, s.ids) {
		t.Fatalf("resume scope incorrect: copied=%v audited=%v", s.copied, s.audited)
	}
	s.warm[1] = false
	if execute(context.Background(), c, s, func(event) {}) == nil {
		t.Fatal("resume incorrectly hid an earlier projection gap")
	}
}

func TestPartialBatchNeverAdvancesCheckpoint(t *testing.T) {
	c := validConfig()
	c.Mode = "apply"
	s := &fakeStore{ids: []int{1, 7}, warm: map[int]bool{}, failCopy: true}
	var events []event
	if execute(context.Background(), c, s, func(e event) { events = append(events, e) }) == nil {
		t.Fatal("partial batch failure ignored")
	}
	for _, e := range events {
		if e.Stage == "copied" || e.Stage == "complete" {
			t.Fatal("failed batch advanced a resumable checkpoint")
		}
	}
}

func TestEveryProjectionGapBlocksCompletion(t *testing.T) {
	for _, c := range []coverage{{MissingReports: 1}, {MissingExternal: 1}, {MissingWire: 1}, {MissingParents: 1}, {StaleReports: 1}, {StaleExternal: 1}, {StaleWire: 1}} {
		if c.complete() {
			t.Fatalf("accepted gap %+v", c)
		}
	}
}

func TestEmptyScopeCannotClaimRepairComplete(t *testing.T) {
	if execute(context.Background(), validConfig(), &fakeStore{}, func(event) {}) == nil {
		t.Fatal("empty brand scope was reported repaired")
	}
}
