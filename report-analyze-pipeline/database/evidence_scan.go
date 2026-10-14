package database

import (
	"context"
	"fmt"
	"sort"
)

// Cursor contains only sequence boundaries. Found bins remain retry anchors
// until a later read proves them empty or they expire from the same 50k window.
// Aligned 500-position bins bound the in-memory queue to at most 101 entries.
type EvidenceScanCursor struct {
	Before    int64
	RetryHigh []int64
	RetryTurn bool
}

type EvidenceScanBatch struct {
	Items      []PendingEvidence
	NextCursor EvidenceScanCursor
	MoreToScan bool
}

// ScanPendingEvidenceReports reads at most four 500-position ranges. Fresh
// intake is checked first; spare capacity is shared between round-robin retry
// anchors and the sweep so a failed row cannot pin the entire older window.
// MAX is sampled once. Any query failure discards the whole round and leaves
// the caller's cursor unchanged. Restarting safely starts another full sweep.
func (d *Database) ScanPendingEvidenceReports(ctx context.Context, limit int, cursor EvidenceScanCursor) (EvidenceScanBatch, error) {
	return scanPendingEvidence(ctx, limit, cursor, func(ctx context.Context) (int64, error) {
		var seq int64
		err := d.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM reports`).Scan(&seq)
		return seq, err
	}, d.GetPendingEvidenceChunk)
}

type evidenceChunkReader func(context.Context, int64, int64, int) ([]PendingEvidence, error)

func scanPendingEvidence(ctx context.Context, limit int, input EvidenceScanCursor, maxSeq func(context.Context) (int64, error), read evidenceChunkReader) (EvidenceScanBatch, error) {
	failed := EvidenceScanBatch{NextCursor: input}
	if limit < 1 || limit > 50 {
		return failed, fmt.Errorf("evidence batch limit must be between 1 and 50")
	}
	maximum, err := maxSeq(ctx)
	if err != nil {
		return failed, err
	}
	if maximum < 0 {
		return failed, fmt.Errorf("invalid evidence sequence maximum")
	}
	if maximum == 0 {
		return EvidenceScanBatch{Items: []PendingEvidence{}}, nil
	}
	floor := positiveEvidenceBound(maximum - evidenceWindowSize)
	freshLow := positiveEvidenceBound(maximum - evidenceChunkSize)
	cursor := EvidenceScanCursor{Before: input.Before, RetryTurn: input.RetryTurn}
	// Copy, normalize, and expire anchors without mutating the caller on failure.
	for _, high := range input.RetryHigh {
		if high <= 0 || high%evidenceChunkSize != 0 || high-evidenceChunkSize >= maximum || high <= floor {
			continue
		}
		if !containsEvidenceBin(cursor.RetryHigh, high) {
			cursor.RetryHigh = append(cursor.RetryHigh, high)
		}
	}
	if cursor.Before <= floor || cursor.Before > freshLow {
		cursor.Before = freshLow
	}
	items := []PendingEvidence{}
	seen := map[int]bool{}
	add := func(rows []PendingEvidence, budget int) {
		for _, item := range rows {
			if budget == 0 || len(items) == limit {
				break
			}
			if !seen[item.Seq] {
				items = append(items, item)
				seen[item.Seq] = true
				budget--
			}
		}
	}
	fresh, err := read(ctx, freshLow, maximum, limit)
	if err != nil {
		return failed, err
	}
	add(fresh, limit)
	// Keep the entire found fresh range retryable too: a moving MAX can put
	// failed fresh work behind the already advanced sweep cursor.
	if len(fresh) > 0 {
		for high := ((freshLow / evidenceChunkSize) + 1) * evidenceChunkSize; high-evidenceChunkSize < maximum; high += evidenceChunkSize {
			if !containsEvidenceBin(cursor.RetryHigh, high) {
				cursor.RetryHigh = append(cursor.RetryHigh, high)
			}
		}
	}
	chunks := 1
	checked := map[int64]bool{}
	hasSweep := cursor.Before > floor
	remaining := limit - len(items)
	// With one spare slot, alternate retry and sweep. With more, reserve at
	// least one batch slot for sweep instead of letting a permanent retry fill it.
	retry := len(cursor.RetryHigh) > 0 && remaining > 0 && (!hasSweep || remaining > 1 || cursor.RetryTurn)
	if len(cursor.RetryHigh) > 0 && remaining == 1 && hasSweep {
		cursor.RetryTurn = !cursor.RetryTurn
	}
	if retry {
		high := cursor.RetryHigh[0]
		cursor.RetryHigh = cursor.RetryHigh[1:]
		low := maxEvidenceBound(floor, high-evidenceChunkSize)
		upper := minEvidenceBound(maximum, high)
		budget := remaining
		if hasSweep && budget > 1 {
			budget--
		}
		duplicateAllowance := 0
		for _, item := range items {
			if int64(item.Seq) > low && int64(item.Seq) <= upper {
				duplicateAllowance++
			}
		}
		queryLimit := budget + duplicateAllowance
		if queryLimit > limit {
			queryLimit = limit
		}
		rows, err := read(ctx, low, upper, queryLimit)
		if err != nil {
			return failed, err
		}
		chunks++
		checked[high] = true
		if len(rows) > 0 {
			cursor.RetryHigh = append(cursor.RetryHigh, high)
		}
		add(rows, budget)
	}
	for chunks < evidenceChunksPerPoll && len(items) < limit && cursor.Before > floor {
		high := cursor.Before
		binHigh := ((high-1)/evidenceChunkSize + 1) * evidenceChunkSize
		low := maxEvidenceBound(floor, binHigh-evidenceChunkSize)
		if !checked[binHigh] {
			rows, err := read(ctx, low, high, limit-len(items))
			if err != nil {
				return failed, err
			}
			chunks++
			checked[binHigh] = true
			if len(rows) > 0 && !containsEvidenceBin(cursor.RetryHigh, binHigh) {
				cursor.RetryHigh = append(cursor.RetryHigh, binHigh)
			}
			add(rows, limit-len(items))
		}
		// A work-containing bin may be passed only while its retry anchor remains.
		cursor.Before = low
	}
	more := cursor.Before > floor || len(cursor.RetryHigh) > 0
	if cursor.Before <= floor {
		cursor.Before = 0
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Seq > items[j].Seq })
	return EvidenceScanBatch{Items: items, NextCursor: cursor, MoreToScan: more}, nil
}

func positiveEvidenceBound(value int64) int64 { return maxEvidenceBound(0, value) }
func maxEvidenceBound(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func minEvidenceBound(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func containsEvidenceBin(bins []int64, high int64) bool {
	for _, bin := range bins {
		if bin == high {
			return true
		}
	}
	return false
}
