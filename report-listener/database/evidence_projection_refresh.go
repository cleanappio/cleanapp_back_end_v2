package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"cleanapp-common/evidencesourceprojection"
	"github.com/go-sql-driver/mysql"
)

// EvidenceSourceProjectionCoverage describes derived-data freshness only. It
// does not claim that a public request or the reported defect has been fixed.
type EvidenceSourceProjectionCoverage struct {
	Reports        int64 `json:"reports"`
	MissingParents int64 `json:"missing_parents"`
	Missing        int64 `json:"missing_projection"`
	Dirty          int64 `json:"dirty_projection"`
}

type EvidenceSourceProjectionRefresh struct {
	Requested      int `json:"requested"`
	Computed       int `json:"computed"`
	MissingParents int `json:"missing_parents"`
}

func projectionRefreshIDs(ids []int) ([]int, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("source projection batch must contain 1..200 report sequences")
	}
	out := append([]int(nil), ids...)
	sort.Ints(out)
	for i, seq := range out {
		if seq <= 0 || seq > 2147483647 || (i > 0 && seq == out[i-1]) {
			return nil, errors.New("source projection sequences must be positive and unique")
		}
	}
	return out, nil
}

// RefreshEvidenceSourceProjection is an explicit bounded operator operation.
// Generation rows are current-locked BEFORE the transaction's first consistent
// source read. A source writer that is waiting in its invalidation trigger then
// marks the derived row dirty after this transaction commits. Never move an
// earlier snapshot read ahead of EnsureRowsAndLock or lock source parents here.
func RefreshEvidenceSourceProjection(ctx context.Context, db *sql.DB, ids []int) (EvidenceSourceProjectionRefresh, error) {
	ids, err := projectionRefreshIDs(ids)
	if err != nil {
		return EvidenceSourceProjectionRefresh{}, err
	}
	for attempt := 0; ; attempt++ {
		result, err := refreshEvidenceSourceProjectionBatch(ctx, db, ids)
		var databaseErr *mysql.MySQLError
		if err == nil || attempt >= 2 || ctx.Err() != nil || !errors.As(err, &databaseErr) || (databaseErr.Number != 1213 && databaseErr.Number != 1205) {
			return result, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return EvidenceSourceProjectionRefresh{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func refreshEvidenceSourceProjectionBatch(ctx context.Context, db *sql.DB, ids []int) (EvidenceSourceProjectionRefresh, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return EvidenceSourceProjectionRefresh{}, err
	}
	defer tx.Rollback()
	revisions, err := evidencesourceprojection.EnsureRowsAndLock(ctx, tx, ids)
	if err != nil {
		return EvidenceSourceProjectionRefresh{}, err
	}
	// This must be the first consistent read after all generation locks. The
	// same exact source reader serves public cache misses; there is no alternate
	// normalization implementation in this operator tool.
	fresh, err := loadFreshEvidenceSourceRecords(ctx, tx, ids)
	if err != nil {
		return EvidenceSourceProjectionRefresh{}, err
	}
	result := EvidenceSourceProjectionRefresh{Requested: len(ids)}
	for _, seq := range ids {
		if err := ctx.Err(); err != nil {
			return EvidenceSourceProjectionRefresh{}, err
		}
		record, exists := fresh[seq]
		if !exists {
			result.MissingParents++
			continue
		}
		if record.Seq != seq || revisions[seq] == 0 {
			return EvidenceSourceProjectionRefresh{}, errors.New("source projection refresh identity differs")
		}
		stored, err := evidencesourceprojection.Store(ctx, tx, revisions[seq], evidenceProjectionRecord(record))
		if err != nil {
			return EvidenceSourceProjectionRefresh{}, err
		}
		if !stored {
			return EvidenceSourceProjectionRefresh{}, errors.New("source projection revision changed during refresh")
		}
		result.Computed++
	}
	if err := tx.Commit(); err != nil {
		return EvidenceSourceProjectionRefresh{}, err
	}
	return result, nil
}

func evidenceProjectionTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	literal := "0000-00-00 00:00:00.000000"
	if !value.IsZero() {
		literal = value.UTC().Format("2006-01-02 15:04:05.000000")
	}
	return &literal
}

func evidenceProjectionRecord(r evidenceRecord) evidencesourceprojection.Record {
	var ingested *string
	if !r.IngestedAtNull {
		ingested = evidenceProjectionTime(&r.IngestedAt)
	}
	return evidencesourceprojection.Record{Seq: r.Seq, SourcePlatform: r.SourcePlatform, SourceURL: r.SourceURL,
		SourceTimestamp: evidenceProjectionTime(r.PublishedAt), ObservedAt: evidenceProjectionTime(r.ObservedAt), TS: ingested,
		IngestionTimeMissing: r.IngestionTimeMissing, ActorKind: r.ActorKind, FetcherID: r.FetcherID,
		PrivacyRestricted: r.PrivacyRestricted, SourceKey: r.SourceKey, ExternalSource: r.ExternalSource,
		IntakeChannel: r.IntakeChannel, GenerationMethod: r.GenerationMethod, SourceAccountKey: r.SourceAccountKey, IngestedAtNull: r.IngestedAtNull}
}

// AuditEvidenceSourceProjection is read-only. A clean revision is an input to
// release verification, not a substitute for tests or an actual public proof.
func AuditEvidenceSourceProjection(ctx context.Context, db *sql.DB, ids []int) (EvidenceSourceProjectionCoverage, error) {
	ids, err := projectionRefreshIDs(ids)
	if err != nil {
		return EvidenceSourceProjectionCoverage{}, err
	}
	payload, err := json.Marshal(ids)
	if err != nil {
		return EvidenceSourceProjectionCoverage{}, err
	}
	var coverage EvidenceSourceProjectionCoverage
	err = db.QueryRowContext(ctx, `SELECT /*+ MAX_EXECUTION_TIME(8000) */ COUNT(*),
 COALESCE(SUM(r.seq IS NULL),0),COALESCE(SUM(p.seq IS NULL),0),
 COALESCE(SUM(p.seq IS NOT NULL AND NOT COALESCE((`+evidencesourceprojection.CleanPredicate("p")+`),FALSE)),0)
 FROM JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$' ERROR ON ERROR)) ids
 LEFT JOIN reports r ON r.seq=ids.seq
 LEFT JOIN `+evidencesourceprojection.Table+` p ON p.seq=ids.seq`, string(payload)).Scan(&coverage.Reports, &coverage.MissingParents, &coverage.Missing, &coverage.Dirty)
	if err != nil {
		return EvidenceSourceProjectionCoverage{}, fmt.Errorf("audit evidence source projection: %w", err)
	}
	return coverage, nil
}
