package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-listener", []migrator.Step{
		{ID: "0001_fetcher_tables", Description: "create fetcher ingest tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureFetcherTables(ctx, db) }},
		{ID: "0002_report_details", Description: "create report_details table", Up: func(ctx context.Context, db *sql.DB) error { return ensureReportDetailsTable(ctx, db) }},
		{ID: "0003_intelligence_tables", Description: "create intelligence session tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureIntelligenceTables(ctx, db) }},
		{ID: "0004_service_state", Description: "create service_state table", Up: func(ctx context.Context, db *sql.DB) error { return ensureServiceStateTable(ctx, db) }},
		{ID: "0005_report_analysis_utf8mb4", Description: "convert report_analysis to utf8mb4", Up: func(ctx context.Context, db *sql.DB) error { return ensureUTF8MB4(ctx, db) }},
		{ID: "0006_report_analysis_class_valid_seq_index", Description: "ensure report_analysis class-valid-seq index", Up: ensureClassValidSeqIndexStep},
		{ID: "0007_report_analysis_needs_ai_review", Description: "ensure report_analysis needs_ai_review column", Up: ensureNeedsAIReviewColumnStep},
		{ID: "0008_cleanapp_wire_tables", Description: "create CleanApp Wire intake tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureCleanAppWireTables(ctx, db) }},
		{ID: "0009_case_tables", Description: "create case and saved cluster tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseTables(ctx, db) }},
		{ID: "0010_case_email_deliveries", Description: "create case email deliveries table", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseEmailDeliveriesTable(ctx, db) }},
		{ID: "0011_reports_public_id", Description: "add and backfill reports public_id column", Up: func(ctx context.Context, db *sql.DB) error { return ensureReportsPublicID(ctx, db) }},
		{ID: "0012_case_escalation_target_channels", Description: "expand case escalation targets for multimodal contact channels", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseEscalationTargetChannels(ctx, db) }},
		{ID: "0013_wire_submission_actor_columns", Description: "add actor/channel/risk columns to wire submissions", Up: func(ctx context.Context, db *sql.DB) error { return ensureWireSubmissionActorColumns(ctx, db) }},
		{ID: "0014_case_accumulation_columns", Description: "add case accumulation and cluster matching columns", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseAccumulationColumns(ctx, db) }},
		{ID: "0015_case_escalation_target_evidence", Description: "add evidence metadata to case escalation targets", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseEscalationTargetEvidenceColumns(ctx, db) }},
		{ID: "0016_case_contact_routing", Description: "add case contact observations and notify plan tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureCaseContactRoutingTables(ctx, db) }},
		{ID: "0017_report_contact_targets", Description: "create cached report escalation target table", Up: func(ctx context.Context, db *sql.DB) error { return ensureReportContactTargetTables(ctx, db) }},
		{ID: "0018_unified_defect_routing", Description: "add shared routing profiles, endpoint memory, execution tasks, outcomes, and authority rules", Up: func(ctx context.Context, db *sql.DB) error { return ensureUnifiedDefectRoutingTables(ctx, db) }},
		{ID: "0019_notify_quality_tuning", Description: "seed jurisdiction-aware authority rules and execution quality defaults", Up: func(ctx context.Context, db *sql.DB) error { return ensureNotifyQualityTuning(ctx, db) }},
		{ID: "0020_mobile_push_delivery", Description: "create mobile push device registry and report delivery event tables", Up: func(ctx context.Context, db *sql.DB) error { return ensureMobilePushDeliveryTables(ctx, db) }},
	})
}

func ensureClassValidSeqIndexStep(ctx context.Context, db *sql.DB) error {
	exists, err := indexExists(ctx, db, "report_analysis", "idx_report_analysis_class_valid_seq")
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
