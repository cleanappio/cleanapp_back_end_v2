package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validCohort() cohort {
	s := cohort{ThroughSeq: throughSeq, Sequences: make([]int, expectedReports)}
	for i := range s.Sequences {
		s.Sequences[i] = i + 1
	}
	return s
}

func TestCohortValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*cohort)
		valid  bool
	}{
		{"approved bounds", func(*cohort) {}, true},
		{"wrong snapshot", func(s *cohort) { s.ThroughSeq++ }, false},
		{"short cohort", func(s *cohort) { s.Sequences = s.Sequences[:len(s.Sequences)-1] }, false},
		{"duplicate", func(s *cohort) { s.Sequences[1] = s.Sequences[0] }, false},
		{"unsorted", func(s *cohort) { s.Sequences[0], s.Sequences[1] = s.Sequences[1], s.Sequences[0] }, false},
		{"nonpositive", func(s *cohort) { s.Sequences[0] = 0 }, false},
		{"beyond snapshot", func(s *cohort) { s.Sequences[len(s.Sequences)-1] = throughSeq + 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := validCohort()
			tc.modify(&s)
			content, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			seqs, err := validateCohort(content)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && len(seqs) != expectedReports {
				t.Fatal("cohort was truncated")
			}
		})
	}
}

func TestPrivateCohortRejectsChangedContentAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cohort.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCohort(path); err == nil || err.Error() != "cohort_snapshot_hash_mismatch" {
		t.Fatal("changed immutable cohort was accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCohort(path); err == nil || err.Error() != "cohort_snapshot_not_private" {
		t.Fatal("public cohort file was accepted")
	}
}

func TestTerminalAuditRejectsEveryMismatch(t *testing.T) {
	for _, counts := range []auditCounts{
		{ParentRows: 2, HeaderRows: 1},
		{ParentRows: 2, HeaderRows: 2, MissingHeaders: 1},
		{ParentRows: 2, HeaderRows: 2, MissingParents: 1},
		{ParentRows: 2, HeaderRows: 2, StaleHeaders: 1},
	} {
		if counts.equal() {
			t.Fatalf("accepted mismatched audit: %+v", counts)
		}
	}
	if !(auditCounts{ParentRows: 2, HeaderRows: 2}).equal() {
		t.Fatal("matching source/header audit was rejected")
	}
}

func TestCancellationStopsPause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pause(ctx) == nil {
		t.Fatal("cancelled helper kept waiting")
	}
}
