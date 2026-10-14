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

// GetPendingEvidenceReports covers new imports whose existing analysis bypasses
// the main analyzer (for example skip_ai GitHub/X imports). Historic work is an
// explicit bounded backfill; this worker covers the last day of ingestion only.
func (d *Database) GetPendingEvidenceReports(ctx context.Context, limit int) ([]PendingEvidence, error) {
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("evidence batch limit must be between 1 and 50")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT r.seq, (SELECT COALESCE(MAX(brand_name), '') FROM report_analysis WHERE seq = r.seq) FROM reports r FORCE INDEX (idx_reports_evidence_ingested) WHERE r.ts >= UTC_TIMESTAMP() - INTERVAL 1 DAY AND EXISTS (SELECT 1 FROM report_analysis ra WHERE ra.seq = r.seq AND ra.evidence_version = 0) ORDER BY r.ts ASC, r.seq ASC LIMIT ?`, limit)
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

// SaveEvidence fills additive columns only; existing summaries, translations,
// ingestion records, and routing state are preserved.
func (d *Database) SaveEvidence(ctx context.Context, seq int, fields evidence.Fields) error {
	_, err := d.db.ExecContext(ctx, `UPDATE report_analysis SET report_kind = ?, evidence_excerpt = ?, reported_impact = ?, classification_reason = ?, entity_relationship = ?, evidence_version = ? WHERE seq = ? AND COALESCE(evidence_version,0) < 1`, fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, evidence.Version, seq)
	return err
}
