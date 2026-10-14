package database

import (
	"cleanapp-common/evidence"
	"context"
	"fmt"
)

type PendingEvidence struct {
	Seq   int
	Brand string
}

const evidenceWindowSize int64 = 50000
const evidenceChunkSize int64 = 500
const evidenceChunksPerPoll = 4

// CASE keeps completed evidence outside analysis/source lookups. Explicit
// sequence bounds contain at most 500 positions and PRIMARY descending order
// avoids materializing/sorting the entire 50000-position worker window.
const pendingEvidenceChunkQuery = `SELECT /*+ MAX_EXECUTION_TIME(8000) */ r.seq,
 (SELECT COALESCE(MAX(brand_name), '') FROM report_analysis WHERE seq = r.seq)
 FROM reports r FORCE INDEX (PRIMARY) LEFT JOIN report_evidence ev ON ev.seq = r.seq
 WHERE r.seq > ? AND r.seq <= ?
 AND CASE WHEN COALESCE(ev.evidence_version, 0) = 0 THEN
   (SELECT 1 FROM report_analysis ra WHERE ra.seq = r.seq LIMIT 1) IS NOT NULL
   AND (r.ts >= UTC_TIMESTAMP() - INTERVAL 1 DAY
     OR EXISTS (SELECT 1 FROM wire_submissions_raw ws WHERE ws.report_seq = r.seq AND ws.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY)
     OR EXISTS (SELECT 1 FROM external_ingest_index ei WHERE ei.seq = r.seq AND ei.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY))
   ELSE FALSE END
 ORDER BY r.seq DESC LIMIT ?`

// GetPendingEvidenceChunk selects within one bounded range of the same recent
// evidence window. The worker samples MAX once per round and never changes
// original publication, server-ingestion eligibility, or analysis/routing state.
func (d *Database) GetPendingEvidenceChunk(ctx context.Context, low, high int64, limit int) ([]PendingEvidence, error) {
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("evidence batch limit must be between 1 and 50")
	}
	if low < 0 || high < low || high-low > evidenceChunkSize {
		return nil, fmt.Errorf("evidence sequence chunk must contain at most 500 positions")
	}
	rows, err := d.db.QueryContext(ctx, pendingEvidenceChunkQuery, low, high, limit)
	if err != nil {
		return nil, fmt.Errorf("query bounded pending evidence (explicit migrations required): %w", err)
	}
	defer rows.Close()
	pending := []PendingEvidence{}
	for rows.Next() {
		var item PendingEvidence
		if err := rows.Scan(&item.Seq, &item.Brand); err != nil {
			return nil, err
		}
		pending = append(pending, item)
	}
	return pending, rows.Err()
}

// SaveEvidence writes once per report to separate storage; existing summaries,
// translations, ingestion records, and routing state are preserved. A failed
// enrichment uses version0 and remains eligible for the recent worker.
func (d *Database) SaveEvidence(ctx context.Context, seq int, fields evidence.Fields, version int) error {
	return evidence.Save(ctx, d.db, seq, fields, version)
}
