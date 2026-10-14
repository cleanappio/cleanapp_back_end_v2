package database

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
)

type evidenceScanFixture struct {
	maximum  int64
	eligible map[int]bool
	checked  []int64
	reads    [][2]int64
	failAt   int
}

func (f *evidenceScanFixture) max(context.Context) (int64, error) { return f.maximum, nil }
func (f *evidenceScanFixture) read(ctx context.Context, low, high int64, limit int) ([]PendingEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.reads = append(f.reads, [2]int64{low, high})
	if f.failAt > 0 && len(f.reads) == f.failAt {
		return nil, errors.New("temporary chunk lookup")
	}
	if low < 0 || high-low > 500 || high < low || limit < 1 || limit > 50 {
		panic("unbounded evidence read")
	}
	f.checked = append(f.checked, high)
	seqs := []int{}
	for seq, pending := range f.eligible {
		if pending && int64(seq) > low && int64(seq) <= high {
			seqs = append(seqs, seq)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(seqs)))
	if len(seqs) > limit {
		seqs = seqs[:limit]
	}
	rows := []PendingEvidence{}
	for _, seq := range seqs {
		rows = append(rows, PendingEvidence{Seq: seq})
	}
	return rows, nil
}
func runEvidenceFixtureRound(t *testing.T, f *evidenceScanFixture, limit int, cursor EvidenceScanCursor) EvidenceScanBatch {
	t.Helper()
	f.reads = nil
	batch, err := scanPendingEvidence(context.Background(), limit, cursor, f.max, f.read)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.reads) > 4 || len(batch.Items) > limit || len(batch.NextCursor.RetryHigh) > 101 {
		t.Fatal("round exceeded bounded read/batch/anchor budget")
	}
	for i := 1; i < len(batch.Items); i++ {
		if batch.Items[i-1].Seq <= batch.Items[i].Seq {
			t.Fatal("batch is not descending and unique")
		}
	}
	floor := positiveEvidenceBound(f.maximum - 50000)
	for _, bounds := range f.reads {
		if bounds[0] < floor || bounds[1] > f.maximum {
			t.Fatal("round escaped current50k window")
		}
	}
	return batch
}

func TestEvidenceCursorSweepsWholeWindowWithSparseOldWork(t *testing.T) {
	f := &evidenceScanFixture{maximum: 100023, eligible: map[int]bool{100010: true, 98000: true, 75555: true, 50024: true, 50023: true}}
	want := map[int]bool{100010: true, 98000: true, 75555: true, 50024: true}
	seen := map[int]bool{}
	cursor := EvidenceScanCursor{}
	for poll := 0; poll < 150; poll++ {
		batch := runEvidenceFixtureRound(t, f, 2, cursor)
		cursor = batch.NextCursor
		for _, item := range batch.Items {
			seen[item.Seq] = true
			f.eligible[item.Seq] = false
		}
		if !batch.MoreToScan && len(batch.Items) == 0 {
			break
		}
		if poll == 149 {
			t.Fatal("sweep never completed")
		}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("full-window recall changed: got=%v want=%v", seen, want)
	}
}

func TestEvidenceCursorPermanentFreshAndOlderFailuresDoNotPinSpareCapacity(t *testing.T) {
	for _, limit := range []int{2, 3, 10} {
		f := &evidenceScanFixture{maximum: 100000, eligible: map[int]bool{99999: true, 98001: true, 50100: true}}
		cursor := EvidenceScanCursor{}
		late := false
		for poll := 0; poll < 180; poll++ {
			batch := runEvidenceFixtureRound(t, f, limit, cursor)
			cursor = batch.NextCursor
			for _, item := range batch.Items {
				if item.Seq == 50100 {
					late = true
					f.eligible[item.Seq] = false
				}
			}
			if late {
				break
			}
		}
		if !late {
			t.Fatalf("permanent fresh/older failures pinned sparse sweep at limit%d", limit)
		}
		if len(cursor.RetryHigh) < 2 {
			t.Fatal("failed bins lost retry anchors")
		}
	}
}

func TestEvidenceCursorOneSlotAlternatesRetryAndSweep(t *testing.T) {
	f := &evidenceScanFixture{maximum: 100000, eligible: map[int]bool{98001: true, 50100: true}}
	cursor := EvidenceScanCursor{}
	late := false
	for poll := 0; poll < 250; poll++ {
		batch := runEvidenceFixtureRound(t, f, 1, cursor)
		cursor = batch.NextCursor
		for _, item := range batch.Items {
			if item.Seq == 50100 {
				late = true
				break
			}
		}
		if late {
			break
		}
	}
	if !late {
		t.Fatal("single-slot permanent older retry starved sweep")
	}
}

func TestEvidenceCursorFullFreshBatchRetainsPriorityAndAllRetryAnchors(t *testing.T) {
	f := &evidenceScanFixture{maximum: 100023, eligible: map[int]bool{100010: true, 98001: true}}
	cursor := EvidenceScanCursor{Before: 80000}
	batch := runEvidenceFixtureRound(t, f, 1, cursor)
	if len(f.reads) != 1 || batch.Items[0].Seq != 100010 || batch.NextCursor.Before != 80000 || len(batch.NextCursor.RetryHigh) != 2 {
		t.Fatal("full fresh batch lost priority/older position/fresh anchors")
	}
}

func TestEvidenceCursorMovingMaxRetainsFailedFreshBinAndRevisitsSourceUpdates(t *testing.T) {
	f := &evidenceScanFixture{maximum: 100000, eligible: map[int]bool{99999: true}}
	cursor := EvidenceScanCursor{Before: 80000}
	first := runEvidenceFixtureRound(t, f, 3, cursor)
	cursor = first.NextCursor
	f.maximum = 101000
	f.eligible[100999] = true
	f.eligible[100250] = true
	f.eligible[97001] = true // old source update behind an advanced sweep
	foundFailed, foundNew, foundUpdated, foundGap := false, false, false, false
	for poll := 0; poll < 160; poll++ {
		batch := runEvidenceFixtureRound(t, f, 3, cursor)
		cursor = batch.NextCursor
		for _, item := range batch.Items {
			switch item.Seq {
			case 99999:
				foundFailed = true
			case 100999:
				foundNew = true
				f.eligible[item.Seq] = false
			case 100250:
				foundGap = true
				f.eligible[item.Seq] = false
			case 97001:
				foundUpdated = true
				f.eligible[item.Seq] = false
			}
		}
		if foundFailed && foundNew && foundUpdated && foundGap {
			break
		}
	}
	if !foundFailed || !foundNew || !foundUpdated || !foundGap {
		t.Fatal("MAX movement dropped failed fresh/new intake/old source update")
	}
}

func TestEvidenceCursorEmptyProofExpiryAndRestartAreSafe(t *testing.T) {
	f := &evidenceScanFixture{maximum: 500, eligible: map[int]bool{1: true}}
	batch := runEvidenceFixtureRound(t, f, 10, EvidenceScanCursor{})
	if len(batch.NextCursor.RetryHigh) != 1 {
		t.Fatal("found small-table range not anchored")
	}
	f.eligible[1] = false
	empty := runEvidenceFixtureRound(t, f, 10, batch.NextCursor)
	if empty.MoreToScan || len(empty.NextCursor.RetryHigh) != 0 {
		t.Fatal("empty proof did not clear completed anchor")
	}
	f.maximum = 100000
	expired := runEvidenceFixtureRound(t, f, 10, EvidenceScanCursor{Before: 1, RetryHigh: []int64{500}})
	if containsEvidenceBin(expired.NextCursor.RetryHigh, 500) {
		t.Fatal("expired anchor retained outside current window")
	}
	f.eligible[55001] = true
	restarted := EvidenceScanCursor{}
	found := false
	for poll := 0; poll < 100; poll++ {
		next := runEvidenceFixtureRound(t, f, 10, restarted)
		restarted = next.NextCursor
		for _, item := range next.Items {
			if item.Seq == 55001 {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("restart skipped unfinished eligible work")
	}
}

func TestEvidenceCursorFailureDiscardsPartialBatchAndKeepsCallerState(t *testing.T) {
	original := EvidenceScanCursor{Before: 80000, RetryHigh: []int64{98000, 97500}, RetryTurn: true}
	saved := EvidenceScanCursor{Before: original.Before, RetryHigh: append([]int64(nil), original.RetryHigh...), RetryTurn: true}
	f := &evidenceScanFixture{maximum: 100000, eligible: map[int]bool{99999: true}, failAt: 3}
	batch, err := scanPendingEvidence(context.Background(), 10, original, f.max, f.read)
	if err == nil || len(batch.Items) != 0 || !reflect.DeepEqual(batch.NextCursor, original) || !reflect.DeepEqual(original, saved) {
		t.Fatal("failed round leaked work or advanced/modified cursor")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanPendingEvidence(canceled, 10, original, f.max, f.read); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown did not stop bounded scan")
	}
}

func TestEvidenceCursorZeroSmallUnsignedAndAlignedAnchorBound(t *testing.T) {
	for _, maximum := range []int64{0, 1, 499, 500, 501, 49999, 50000, 50001} {
		f := &evidenceScanFixture{maximum: maximum, eligible: map[int]bool{}}
		batch := runEvidenceFixtureRound(t, f, 50, EvidenceScanCursor{})
		if maximum <= 500 && batch.MoreToScan {
			t.Fatal("small empty unsigned table requested incomplete scan")
		}
	}
	f := &evidenceScanFixture{maximum: 100023, eligible: map[int]bool{}}
	anchors := []int64{}
	for high := int64(500); high <= 101000; high += 500 {
		anchors = append(anchors, high, high)
	}
	batch := runEvidenceFixtureRound(t, f, 10, EvidenceScanCursor{RetryHigh: anchors})
	if len(batch.NextCursor.RetryHigh) > 101 {
		t.Fatal("moving-window grid did not bound retry anchors")
	}
}
