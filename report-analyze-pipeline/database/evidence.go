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

const pendingEvidenceQuery = `WITH pending_candidates AS (
 SELECT candidate.seq
 FROM reports candidate LEFT JOIN report_evidence ev ON ev.seq = candidate.seq
 WHERE candidate.seq > GREATEST(0, CAST((SELECT COALESCE(MAX(seq), 0) FROM reports) AS SIGNED) - 50000)
 AND COALESCE(ev.evidence_version, 0) = 0
 ORDER BY candidate.seq DESC LIMIT 50000
)
 SELECT /*+ MAX_EXECUTION_TIME(8000) */ r.seq,
 (SELECT COALESCE(MAX(brand_name), '') FROM report_analysis WHERE seq = r.seq)
 FROM pending_candidates c STRAIGHT_JOIN reports r ON r.seq = c.seq
 WHERE (SELECT 1 FROM report_analysis ra WHERE ra.seq = r.seq LIMIT 1) IS NOT NULL
 AND (r.ts >= UTC_TIMESTAMP() - INTERVAL 1 DAY
   OR EXISTS (SELECT 1 FROM wire_submissions_raw ws WHERE ws.report_seq = r.seq AND ws.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY)
   OR EXISTS (SELECT 1 FROM external_ingest_index ei WHERE ei.seq = r.seq AND ei.created_at >= UTC_TIMESTAMP() - INTERVAL 1 DAY))
 ORDER BY r.seq DESC LIMIT ?`

// GetPendingEvidenceReports covers new imports whose existing analysis bypasses
// the main analyzer (for example skip_ai GitHub/X imports). Historic work is an
// explicit bounded backfill. Only the newest 50,000 sequence values are scanned,
// using the existing reports primary key without a timestamp index or blob scan.
// Recent server-side Wire/index creation also qualifies imports whose source
// publication timestamp is old. Older sequence values require explicit backfill.
// Scalar presence prevents MySQL from materializing a large RA semijoin, while
// descending primary-key order prioritizes fresh reports. The candidate LIMIT
// prevents merging: the integer primary-key window already contains at most
// 50,000 rows, so it preserves every eligible sequence. STRAIGHT_JOIN makes those
// candidates drive analysis-presence and timestamp/source qualification, after
// completed evidence has been excluded, instead of probing stable RA/Wire/index
// rows on every scan.
func (d *Database) GetPendingEvidenceReports(ctx context.Context, limit int) ([]PendingEvidence, error) {
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("evidence batch limit must be between 1 and 50")
	}
	rows, err := d.db.QueryContext(ctx, pendingEvidenceQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending evidence (explicit migrations required): %w", err)
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
