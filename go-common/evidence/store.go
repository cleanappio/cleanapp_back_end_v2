package evidence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Save persists one already source-validated extraction per report sequence,
// shared by every analysis language. Failed version 0 attempts remain retryable
// and cannot overwrite a completed version. Equal versions permit force reviews.
func Save(ctx context.Context, db *sql.DB, seq int, fields Fields, version int) error {
	if seq <= 0 || version < 0 {
		return fmt.Errorf("evidence storage requires a positive report sequence and nonnegative version")
	}
	fields.ReportKind = NormalizeKind(fields.ReportKind)
	fields.EntityRelationship = NormalizeRelationship(fields.EntityRelationship)
	fields.EvidenceExcerpt = Bound(fields.EvidenceExcerpt, MaxExcerptRunes)
	fields.ReportedImpact = Bound(strings.TrimSpace(fields.ReportedImpact), 500)
	fields.ClassificationReason = Bound(strings.TrimSpace(fields.ClassificationReason), 500)
	// MySQL evaluates assignments left to right. Advance the stored version only
	// after every field has compared against the original stored version.
	_, err := db.ExecContext(ctx, `INSERT INTO report_evidence
		(seq, report_kind, evidence_excerpt, reported_impact, classification_reason, entity_relationship, evidence_version)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		report_kind = IF(VALUES(evidence_version) >= evidence_version, VALUES(report_kind), report_kind),
		evidence_excerpt = IF(VALUES(evidence_version) >= evidence_version, VALUES(evidence_excerpt), evidence_excerpt),
		reported_impact = IF(VALUES(evidence_version) >= evidence_version, VALUES(reported_impact), reported_impact),
		classification_reason = IF(VALUES(evidence_version) >= evidence_version, VALUES(classification_reason), classification_reason),
		entity_relationship = IF(VALUES(evidence_version) >= evidence_version, VALUES(entity_relationship), entity_relationship),
		evidence_version = GREATEST(evidence_version, VALUES(evidence_version))`,
		seq, fields.ReportKind, fields.EvidenceExcerpt, fields.ReportedImpact, fields.ClassificationReason, fields.EntityRelationship, version)
	if err != nil {
		return fmt.Errorf("save per-report evidence: %w", err)
	}
	return nil
}
