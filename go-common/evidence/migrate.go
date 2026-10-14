package evidence

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate is called only by explicit service cmd/migrate entrypoints. Evidence
// lives in a separate per-report table; existing FULLTEXT analysis tables and
// migration history are never inspected, altered or rewritten here.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS report_evidence (
		seq INT NOT NULL PRIMARY KEY,
		report_kind VARCHAR(16) NOT NULL DEFAULT 'unknown',
		evidence_excerpt TEXT NULL,
		reported_impact TEXT NULL,
		classification_reason TEXT NULL,
		entity_relationship VARCHAR(16) NOT NULL DEFAULT 'unknown',
		evidence_version INT NOT NULL DEFAULT 0,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		KEY idx_report_evidence_kind_version (report_kind, evidence_version)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return fmt.Errorf("create per-report evidence table: %w", err)
	}
	return nil
}
