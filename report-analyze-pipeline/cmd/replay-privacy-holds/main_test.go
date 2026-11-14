package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cleanapp-common/reportoutcome"
)

type fakeRepository struct {
	values                 map[int]snapshot
	loads, locks, releases int
	locked, published      bool
	loadError              bool
}

func validSnapshot(seq int) snapshot {
	return snapshot{Entry: entry{seq, "rcpt_fixture_" + strings.Repeat("a", 24), strings.Repeat("b", 64), strings.Repeat("c", 64)},
		ReceiptOK: true, Private: true, Unpublished: true, NoAnalysis: true, NoImage: true, Held: true, Applicable: true,
		Input: reportoutcome.Input{ConsentVersion: 1, Handling: reportoutcome.Handling{Publication: "automatic", Notices: "automatic"}, HasOriginal: true, HasSourceContext: true}}
}
func (f *fakeRepository) Load(_ context.Context, seq int) (snapshot, error) {
	f.loads++
	if f.loadError {
		return snapshot{}, errors.New("PRIVATE database diagnostics")
	}
	return f.values[seq], nil
}
func (f *fakeRepository) Lock(context.Context, int) (func() error, bool, error) {
	f.locks++
	f.locked = true
	return func() error { f.locked = false; f.releases++; return nil }, f.published, nil
}
func fixture(seqs ...int) (*fakeRepository, manifest, journal) {
	f := &fakeRepository{values: map[int]snapshot{}}
	m := manifest{Kind: manifestKind, Reason: reason}
	for _, seq := range seqs {
		s := validSnapshot(seq)
		f.values[seq] = s
		m.Items = append(m.Items, s.Entry)
	}
	return f, m, journal{ManifestHash: digest(m), Items: map[int]string{}}
}

func TestExplicitSequenceBoundAndManifestBinding(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "1,1", "1,", "2147483648", "1,2,3,4,5,6,7,8,9,10,11"} {
		if _, err := parseSeqs(raw); err == nil {
			t.Fatalf("invalid sequence set accepted: %q", raw)
		}
	}
	seqs, err := parseSeqs("2, 1")
	if err != nil || !reflect.DeepEqual(seqs, []int{1, 2}) {
		t.Fatal("explicit sequences not normalized")
	}
	_, m, _ := fixture(1, 2)
	if validateManifest(m, seqs) != nil {
		t.Fatal("valid manifest rejected")
	}
	m.Items[0].ReceiptID = "different"
	if validateManifest(m, seqs) == nil {
		t.Fatal("manifest identity not checked")
	}
}
func TestDryReviewOnlyReadsAndEmitsNoContent(t *testing.T) {
	f, _, _ := fixture(1)
	m, err := review(context.Background(), f, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if f.loads != 1 || f.locks != 0 || f.releases != 0 || len(m.Items) != 1 {
		t.Fatal("dry review has unexpected side effects")
	}
	raw, _ := json.Marshal(m)
	for _, private := range []string{"tracking_token", "description", "report_json", "PRIVATE"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("manifest includes private content")
		}
	}
	f.loadError = true
	_, err = review(context.Background(), f, []int{1})
	if err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("upstream diagnostics leaked")
	}
}
func TestEveryRecheckGuardFailsClosed(t *testing.T) {
	tests := []func(*snapshot){
		func(s *snapshot) { s.ReceiptOK = false }, func(s *snapshot) { s.Private = false }, func(s *snapshot) { s.Unpublished = false },
		func(s *snapshot) { s.NoAnalysis = false }, func(s *snapshot) { s.NoImage = false }, func(s *snapshot) { s.Held = false },
		func(s *snapshot) { s.Applicable = false }, func(s *snapshot) { s.Input.ConsentVersion = 0 },
		func(s *snapshot) { s.Input.Handling.Publication = "private" }, func(s *snapshot) { s.Input.Handling.Notices = "none" },
		func(s *snapshot) { s.Input.AnalysisSuppressed = true }, func(s *snapshot) { s.Input.PolicyHoldReason = "policy_hold" },
		func(s *snapshot) { s.Input.PrivacyHoldReason = reason }, func(s *snapshot) { s.Input.HasOriginal = false }, func(s *snapshot) { s.Input.HasSourceContext = false },
	}
	for i, change := range tests {
		f, m, j := fixture(1)
		s := f.values[1]
		change(&s)
		f.values[1] = s
		publishes, writes := 0, 0
		result := replay(context.Background(), f, m, &j, func() error { writes++; return nil }, func(int, string) error { publishes++; return nil })
		if result.Error == "" || publishes != 0 || writes != 0 || f.releases != 1 {
			t.Fatalf("guard %d did not fail closed", i)
		}
	}
}
func TestApplyBindsReceiptAndBothHashesUnderLock(t *testing.T) {
	for _, change := range []func(*entry){func(e *entry) { e.ReceiptID += "changed" }, func(e *entry) { e.SourceHash = strings.Repeat("d", 64) }, func(e *entry) { e.StateHash = strings.Repeat("d", 64) }} {
		f, m, j := fixture(1)
		s := f.values[1]
		change(&s.Entry)
		f.values[1] = s
		result := replay(context.Background(), f, m, &j, func() error { t.Fatal("journal write before source match"); return nil }, func(int, string) error { t.Fatal("publish before source match"); return nil })
		if result.Error != "reviewed_source_or_state_changed" || f.releases != 1 {
			t.Fatal("reviewed binding was bypassed")
		}
	}
}
func TestConfirmedPublishHasDurableAttemptAndNoDuplicateReplay(t *testing.T) {
	f, m, j := fixture(1)
	writes, publishes := 0, 0
	persist := func() error {
		if !f.locked {
			t.Fatal("journal written outside report lock")
		}
		writes++
		return nil
	}
	publish := func(seq int, id string) error {
		publishes++
		if !f.locked || writes != 1 || j.Items[seq] != "attempted" || id != "privacy-hold-replay:v1:1:"+m.Items[0].SourceHash {
			t.Fatal("publish was not fenced or stable")
		}
		return nil
	}
	result := replay(context.Background(), f, m, &j, persist, publish)
	if result.Error != "" || !reflect.DeepEqual(result.Confirmed, []int{1}) || j.Items[1] != "confirmed" || publishes != 1 || writes != 2 || f.releases != 1 || result.ProviderCalls != 0 {
		t.Fatalf("unexpected confirmed result: %+v", result)
	}
	result = replay(context.Background(), f, m, &j, persist, publish)
	if result.Error != "" || !reflect.DeepEqual(result.AlreadyConfirmed, []int{1}) || publishes != 1 || f.locks != 1 {
		t.Fatal("confirmed enqueue was repeated")
	}
}
func TestUncertainPublishStopsAndCannotAutomaticallyRetry(t *testing.T) {
	f, m, j := fixture(1, 2, 3)
	publishes := 0
	publish := func(seq int, _ string) error {
		publishes++
		if seq == 2 {
			return errors.New("PRIVATE broker error")
		}
		return nil
	}
	result := replay(context.Background(), f, m, &j, func() error { return nil }, publish)
	if result.Error != "broker_outcome_uncertain" || publishes != 2 || !reflect.DeepEqual(result.Confirmed, []int{1}) || j.Items[2] != "attempted" || j.Items[3] != "" {
		t.Fatalf("partial failure not bounded: %+v", result)
	}
	result = replay(context.Background(), f, m, &j, func() error { return nil }, publish)
	if result.Error != "prior_attempt_requires_operator_review" || publishes != 2 {
		t.Fatal("uncertain enqueue was automatically retried")
	}
}
func TestJournalFailuresNeverInviteBlindResend(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		f, m, j := fixture(1)
		writes, publishes := 0, 0
		durable := journal{ManifestHash: j.ManifestHash, Items: map[int]string{}}
		result := replay(context.Background(), f, m, &j, func() error {
			writes++
			if writes == failAt {
				return errors.New("journal failure")
			}
			durable.Items[1] = j.Items[1]
			return nil
		}, func(int, string) error { publishes++; return nil })
		if failAt == 1 && (publishes != 0 || result.Error != "journal_write_failed") {
			t.Fatal("published without durable fence")
		}
		if failAt == 2 {
			if publishes != 1 || result.Error != "confirmed_journal_write_failed" || durable.Items[1] != "attempted" {
				t.Fatal("ACK/journal gap was not retained")
			}
			replay(context.Background(), f, m, &durable, func() error { return nil }, func(int, string) error { t.Fatal("ACK gap blindly resent"); return nil })
		}
	}
}
func TestPrivateJournalAndManifestFiles(t *testing.T) {
	_, m, j := fixture(1)
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.json")
	if err := saveJournal(path, j); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("journal not private")
	}
	var loaded journal
	if readPrivateJSON(path, &loaded) != nil || loaded.ManifestHash != digest(m) {
		t.Fatal("journal did not round trip")
	}
	if os.Chmod(path, 0644) != nil {
		t.Fatal("chmod fixture failed")
	}
	if readPrivateJSON(path, &loaded) == nil {
		t.Fatal("public-readable journal accepted")
	}
	if os.Chmod(path, 0600) != nil {
		t.Fatal("chmod fixture failed")
	}
	link := filepath.Join(dir, "link.json")
	if os.Symlink(path, link) != nil {
		t.Fatal("symlink fixture failed")
	}
	if readPrivateJSON(link, &loaded) == nil {
		t.Fatal("symlink accepted")
	}
	if os.WriteFile(path, []byte(`{"unknown":"field"}`), 0600) != nil {
		t.Fatal("write fixture failed")
	}
	if readPrivateJSON(path, &loaded) == nil {
		t.Fatal("unknown journal fields accepted")
	}
}
