package database

import (
	"context"
	"fmt"
)

// Exact previous full-window selector is a fixture oracle only.
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

func (d *Database) GetPendingEvidenceReports(ctx context.Context, limit int) ([]PendingEvidence, error) {
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid evidence batch limit")
	}
	rows, err := d.db.QueryContext(ctx, pendingEvidenceQuery, limit)
	if err != nil {
		return nil, err
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
