package evidence

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate is called only by explicit service cmd/migrate entrypoints. It never
// runs during analysis or API startup. Both services may safely call it.
func Migrate(ctx context.Context, db *sql.DB) error {
	columns := []struct{ name, definition string }{
		{"report_kind", "VARCHAR(16) NOT NULL DEFAULT 'unknown'"},
		{"evidence_excerpt", "TEXT NULL"},
		{"reported_impact", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"classification_reason", "VARCHAR(500) NOT NULL DEFAULT ''"},
		{"entity_relationship", "VARCHAR(16) NOT NULL DEFAULT 'unknown'"},
		{"evidence_version", "INT NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'report_analysis' AND COLUMN_NAME = ?`, column.name).Scan(&count); err != nil {
			return fmt.Errorf("check evidence column %s: %w", column.name, err)
		}
		if count > 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE report_analysis ADD COLUMN "+column.name+" "+column.definition); err != nil {
			return fmt.Errorf("add evidence column %s: %w", column.name, err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'report_analysis' AND INDEX_NAME = 'idx_report_analysis_brand_kind_language'`).Scan(&count); err != nil {
		return fmt.Errorf("check evidence index: %w", err)
	}
	if count == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE report_analysis ADD INDEX idx_report_analysis_brand_kind_language (brand_name, report_kind, language, is_valid, seq)`); err != nil {
			return fmt.Errorf("add evidence index: %w", err)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'reports' AND INDEX_NAME = 'idx_reports_evidence_ingested'`).Scan(&count); err != nil {
		return fmt.Errorf("check evidence ingestion index: %w", err)
	}
	if count == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE reports ADD INDEX idx_reports_evidence_ingested (ts, seq)`); err != nil {
			return fmt.Errorf("add evidence ingestion index: %w", err)
		}
	}
	return nil
}
