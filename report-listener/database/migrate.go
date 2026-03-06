package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-listener", []migrator.Step{
		{ID: "0001_fetcher_tables", Description: "create fetcher ingest tables", Up: ensureFetcherTablesStep},
		{ID: "0002_report_details", Description: "create report_details table", Up: ensureReportDetailsTableStep},
		{ID: "0003_intelligence_tables", Description: "create intelligence session tables", Up: ensureIntelligenceTablesStep},
		{ID: "0004_service_state", Description: "create service_state table", Up: ensureServiceStateTableStep},
		{ID: "0005_report_analysis_utf8mb4", Description: "convert report_analysis to utf8mb4", Up: ensureUTF8MB4Step},
		{ID: "0006_report_analysis_class_valid_seq_index", Description: "ensure report_analysis class-valid-seq index", Up: ensureClassValidSeqIndexStep},
		{ID: "0007_report_analysis_needs_ai_review", Description: "ensure report_analysis needs_ai_review column", Up: ensureNeedsAIReviewColumnStep},
	})
}

func ensureFetcherTablesStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureFetcherTables(ctx)
}

func ensureReportDetailsTableStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureReportDetailsTable(ctx)
}

func ensureIntelligenceTablesStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureIntelligenceTables(ctx)
}

func ensureServiceStateTableStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureServiceStateTable(ctx)
}

func ensureUTF8MB4Step(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureUTF8MB4(ctx)
}

func ensureClassValidSeqIndexStep(ctx context.Context, db *sql.DB) error {
	d := &Database{db: db}
	exists, err := d.IndexExists(ctx, "report_analysis", "idx_report_analysis_class_valid_seq")
	if err != nil {
		return fmt.Errorf("failed to check report_analysis index: %w", err)
	}
	if exists {
		return nil
	}
	_, err = db.ExecContext(ctx, `
		ALTER TABLE report_analysis
		ADD INDEX idx_report_analysis_class_valid_seq (classification, is_valid, seq)
	`)
	if err != nil {
		return fmt.Errorf("failed to create report_analysis class_valid_seq index: %w", err)
	}
	return nil
}

func ensureNeedsAIReviewColumnStep(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = 'report_analysis'
		  AND COLUMN_NAME = 'needs_ai_review'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check needs_ai_review column: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, `
		ALTER TABLE report_analysis
		ADD COLUMN needs_ai_review BOOL NOT NULL DEFAULT FALSE
	`)
	if err != nil {
		return fmt.Errorf("failed to add needs_ai_review column: %w", err)
	}
	return nil
}
