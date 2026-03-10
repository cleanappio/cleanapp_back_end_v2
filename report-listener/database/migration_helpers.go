package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"report-listener/publicid"
)

func ensureUTF8MB4(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE report_analysis CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			log.Printf("warn: utf8mb4 convert skipped: %v", err)
		}
	}
	return nil
}

func indexExists(ctx context.Context, db *sql.DB, tableName, indexName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = ?
		AND INDEX_NAME = ?`,
		tableName, indexName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if index exists: %w", err)
	}
	return count > 0, nil
}

func columnExists(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = ?
		AND COLUMN_NAME = ?`,
		tableName, columnName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if column exists: %w", err)
	}
	return count > 0, nil
}

func ensureReportsPublicID(ctx context.Context, db *sql.DB) error {
	exists, err := columnExists(ctx, db, "reports", "public_id")
	if err != nil {
		return fmt.Errorf("failed to check reports.public_id column: %w", err)
	}
	if !exists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE reports
			ADD COLUMN public_id VARCHAR(32) NULL AFTER seq
		`); err != nil {
			return fmt.Errorf("failed to add reports.public_id column: %w", err)
		}
	}

	for {
		filled, err := backfillReportPublicIDs(ctx, db, 1000)
		if err != nil {
			return err
		}
		cleared, err := clearDuplicateReportPublicIDs(ctx, db)
		if err != nil {
			return err
		}
		if filled == 0 && cleared == 0 {
			break
		}
	}

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE reports
		MODIFY COLUMN public_id VARCHAR(32) NOT NULL
	`); err != nil {
		return fmt.Errorf("failed to enforce reports.public_id NOT NULL: %w", err)
	}

	indexReady, err := indexExists(ctx, db, "reports", "uq_reports_public_id")
	if err != nil {
		return fmt.Errorf("failed to check reports.public_id index: %w", err)
	}
	if !indexReady {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE reports
			ADD UNIQUE INDEX uq_reports_public_id (public_id)
		`); err != nil {
			return fmt.Errorf("failed to add reports.public_id unique index: %w", err)
		}
	}

	return nil
}

func backfillReportPublicIDs(ctx context.Context, db *sql.DB, batchSize int) (int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq
		FROM reports
		WHERE public_id IS NULL OR public_id = ''
		ORDER BY seq ASC
		LIMIT ?
	`, batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to load reports missing public_id: %w", err)
	}
	defer rows.Close()

	var seqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return 0, fmt.Errorf("failed to scan report seq for public_id backfill: %w", err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("failed iterating reports missing public_id: %w", err)
	}
	if len(seqs) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to start public_id backfill transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE reports
		SET public_id = ?
		WHERE seq = ? AND (public_id IS NULL OR public_id = '')
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare public_id backfill statement: %w", err)
	}
	defer stmt.Close()

	used := make(map[string]struct{}, len(seqs))
	for _, seq := range seqs {
		publicID, err := nextReportPublicID(used)
		if err != nil {
			return 0, err
		}
		if _, err := stmt.ExecContext(ctx, publicID, seq); err != nil {
			return 0, fmt.Errorf("failed to update report %d public_id: %w", seq, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit public_id backfill: %w", err)
	}
	return len(seqs), nil
}

func clearDuplicateReportPublicIDs(ctx context.Context, db *sql.DB) (int, error) {
	var duplicatePublicID string
	err := db.QueryRowContext(ctx, `
		SELECT public_id
		FROM reports
		WHERE public_id IS NOT NULL AND public_id <> ''
		GROUP BY public_id
		HAVING COUNT(*) > 1
		LIMIT 1
	`).Scan(&duplicatePublicID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to detect duplicate report public_id values: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT seq
		FROM reports
		WHERE public_id = ?
		ORDER BY seq ASC
	`, duplicatePublicID)
	if err != nil {
		return 0, fmt.Errorf("failed to load duplicate public_id rows: %w", err)
	}
	defer rows.Close()

	var seqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return 0, fmt.Errorf("failed to scan duplicate public_id seq: %w", err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("failed iterating duplicate public_id rows: %w", err)
	}
	if len(seqs) <= 1 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to start duplicate public_id cleanup transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE reports SET public_id = ? WHERE seq = ?`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare duplicate public_id cleanup statement: %w", err)
	}
	defer stmt.Close()

	used := map[string]struct{}{duplicatePublicID: {}}
	for _, seq := range seqs[1:] {
		publicID, err := nextReportPublicID(used)
		if err != nil {
			return 0, err
		}
		if _, err := stmt.ExecContext(ctx, publicID, seq); err != nil {
			return 0, fmt.Errorf("failed to replace duplicate public_id for report %d: %w", seq, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit duplicate public_id cleanup: %w", err)
	}
	return len(seqs) - 1, nil
}

func nextReportPublicID(used map[string]struct{}) (string, error) {
	for {
		publicID, err := publicid.NewReportID()
		if err != nil {
			return "", fmt.Errorf("failed to generate report public_id: %w", err)
		}
		if _, exists := used[publicID]; exists {
			continue
		}
		used[publicID] = struct{}{}
		return publicID, nil
	}
}

func ensureFetcherTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS fetchers (
            id INT UNSIGNED AUTO_INCREMENT,
            fetcher_id VARCHAR(64) NOT NULL UNIQUE,
            name VARCHAR(255) NOT NULL,
            token_hash VARBINARY(64) NOT NULL,
            scopes JSON NULL,
            active BOOL NOT NULL DEFAULT TRUE,
            last_used_at TIMESTAMP NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (id),
            INDEX idx_active (active)
        )`,
		`CREATE TABLE IF NOT EXISTS external_ingest_index (
            source VARCHAR(64) NOT NULL,
            external_id VARCHAR(255) NOT NULL,
            seq INT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (source, external_id),
            INDEX idx_seq (seq)
        )`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure table: %w", err)
		}
	}
	return nil
}

func ensureReportDetailsTable(ctx context.Context, db *sql.DB) error {
	stmt := `
        CREATE TABLE IF NOT EXISTS report_details (
            seq INT NOT NULL,
            company_name VARCHAR(255),
            product_name VARCHAR(255),
            url VARCHAR(512),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (seq),
            CONSTRAINT fk_report_details_seq FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE,
            INDEX idx_company (company_name),
            INDEX idx_product (product_name)
        )`
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("failed to ensure report_details table: %w", err)
	}
	return nil
}

func ensureServiceStateTable(ctx context.Context, db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS service_state (
			service_name VARCHAR(100) PRIMARY KEY,
			last_processed_seq INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_service_name (service_name)
		)
	`

	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create service_state table: %w", err)
	}
	return nil
}

func ensureIntelligenceTables(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS intelligence_usage (
			session_id VARCHAR(128) PRIMARY KEY,
			turns_used INT NOT NULL DEFAULT 0,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to ensure intelligence_usage table: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS intelligence_session_state (
			session_id VARCHAR(128) PRIMARY KEY,
			last_report_ids_json TEXT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to ensure intelligence_session_state table: %w", err)
	}
	return nil
}

func ensureCleanAppWireTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS wire_submissions_raw (
			submission_id VARCHAR(64) NOT NULL,
			receipt_id VARCHAR(64) NOT NULL,
			fetcher_id VARCHAR(64) NOT NULL,
			key_id CHAR(36) NULL,
			source_id VARCHAR(255) NOT NULL,
			schema_version VARCHAR(32) NOT NULL,
			submitted_at TIMESTAMP NOT NULL,
			observed_at TIMESTAMP NULL,
			agent_id VARCHAR(128) NOT NULL,
			lane VARCHAR(16) NOT NULL,
			material_hash CHAR(64) NOT NULL,
			submission_quality FLOAT NOT NULL DEFAULT 0,
			report_seq INT NULL,
			agent_json JSON NULL,
			provenance_json JSON NULL,
			report_json JSON NULL,
			dedupe_json JSON NULL,
			delivery_json JSON NULL,
			extensions_json JSON NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (submission_id),
			UNIQUE KEY uniq_wire_receipt_id (receipt_id),
			UNIQUE KEY uniq_wire_fetcher_source (fetcher_id, source_id),
			KEY idx_wire_fetcher_created (fetcher_id, created_at),
			KEY idx_wire_lane_created (lane, created_at),
			CONSTRAINT fk_wire_submission_fetcher FOREIGN KEY (fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE,
			CONSTRAINT fk_wire_submission_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS wire_submission_receipts (
			receipt_id VARCHAR(64) NOT NULL,
			submission_id VARCHAR(64) NOT NULL,
			fetcher_id VARCHAR(64) NOT NULL,
			source_id VARCHAR(255) NOT NULL,
			report_seq INT NULL,
			status VARCHAR(32) NOT NULL,
			lane VARCHAR(16) NOT NULL,
			idempotency_replay BOOL NOT NULL DEFAULT FALSE,
			rejection_code VARCHAR(64) NULL,
			warnings_json JSON NULL,
			next_check_after TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (receipt_id),
			UNIQUE KEY uniq_wire_receipt_submission (submission_id),
			KEY idx_wire_receipts_fetcher_source (fetcher_id, source_id, created_at),
			CONSTRAINT fk_wire_receipt_submission FOREIGN KEY (submission_id) REFERENCES wire_submissions_raw(submission_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS wire_agent_reputation_metrics (
			fetcher_id VARCHAR(64) NOT NULL,
			precision_score FLOAT NOT NULL DEFAULT 0.45,
			novelty_score FLOAT NOT NULL DEFAULT 0.45,
			evidence_score FLOAT NOT NULL DEFAULT 0.45,
			routing_score FLOAT NOT NULL DEFAULT 0.45,
			corroboration_score FLOAT NOT NULL DEFAULT 0.45,
			latency_score FLOAT NOT NULL DEFAULT 0.45,
			resolution_score FLOAT NOT NULL DEFAULT 0.45,
			policy_score FLOAT NOT NULL DEFAULT 0.45,
			dedupe_penalty FLOAT NOT NULL DEFAULT 0,
			abuse_penalty FLOAT NOT NULL DEFAULT 0,
			reputation_score FLOAT NOT NULL DEFAULT 0.45,
			sample_size INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (fetcher_id),
			CONSTRAINT fk_wire_reputation_fetcher FOREIGN KEY (fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS wire_issue_clusters (
			cluster_id VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'stub',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (cluster_id)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS wire_reward_records (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			fetcher_id VARCHAR(64) NOT NULL,
			report_seq INT NULL,
			reward_points FLOAT NOT NULL DEFAULT 0,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_wire_reward_fetcher_created (fetcher_id, created_at),
			CONSTRAINT fk_wire_reward_fetcher FOREIGN KEY (fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE,
			CONSTRAINT fk_wire_reward_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure CleanApp Wire table: %w", err)
		}
	}
	return nil
}

func ensureCaseTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS saved_clusters (
			cluster_id VARCHAR(64) NOT NULL,
			source_type VARCHAR(32) NOT NULL,
			classification VARCHAR(32) NOT NULL DEFAULT 'physical',
			geometry_json JSON NULL,
			seed_report_seq INT NULL,
			report_count INT NOT NULL DEFAULT 0,
			summary TEXT NULL,
			stats_json JSON NULL,
			analysis_json JSON NULL,
			created_by_user_id VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (cluster_id),
			KEY idx_saved_clusters_created_by (created_by_user_id, created_at),
			KEY idx_saved_clusters_seed (seed_report_seq),
			CONSTRAINT fk_saved_clusters_seed FOREIGN KEY (seed_report_seq) REFERENCES reports(seq) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS cases (
			case_id VARCHAR(64) NOT NULL,
			slug VARCHAR(128) NOT NULL,
			title VARCHAR(255) NOT NULL,
			type VARCHAR(64) NOT NULL DEFAULT 'incident',
			status VARCHAR(32) NOT NULL DEFAULT 'open',
			classification VARCHAR(32) NOT NULL DEFAULT 'physical',
			summary TEXT NULL,
			uncertainty_notes TEXT NULL,
			geometry_json JSON NULL,
			anchor_report_seq INT NULL,
			anchor_lat DOUBLE NULL,
			anchor_lng DOUBLE NULL,
			building_id VARCHAR(128) NULL,
			parcel_id VARCHAR(128) NULL,
			severity_score FLOAT NOT NULL DEFAULT 0,
			urgency_score FLOAT NOT NULL DEFAULT 0,
			confidence_score FLOAT NOT NULL DEFAULT 0,
			exposure_score FLOAT NOT NULL DEFAULT 0,
			criticality_score FLOAT NOT NULL DEFAULT 0,
			trend_score FLOAT NOT NULL DEFAULT 0,
			first_seen_at TIMESTAMP NULL,
			last_seen_at TIMESTAMP NULL,
			created_by_user_id VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (case_id),
			UNIQUE KEY uniq_cases_slug (slug),
			KEY idx_cases_status_updated (status, updated_at),
			KEY idx_cases_created_by (created_by_user_id, created_at),
			KEY idx_cases_anchor_report (anchor_report_seq),
			CONSTRAINT fk_cases_anchor_report FOREIGN KEY (anchor_report_seq) REFERENCES reports(seq) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_reports (
			case_id VARCHAR(64) NOT NULL,
			seq INT NOT NULL,
			link_reason VARCHAR(128) NOT NULL DEFAULT 'manual',
			confidence FLOAT NOT NULL DEFAULT 1.0,
			attached_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (case_id, seq),
			KEY idx_case_reports_seq (seq),
			CONSTRAINT fk_case_reports_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
			CONSTRAINT fk_case_reports_report FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_clusters (
			case_id VARCHAR(64) NOT NULL,
			cluster_id VARCHAR(64) NOT NULL,
			linked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (case_id, cluster_id),
			CONSTRAINT fk_case_clusters_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
			CONSTRAINT fk_case_clusters_cluster FOREIGN KEY (cluster_id) REFERENCES saved_clusters(cluster_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_escalation_targets (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			role_type VARCHAR(64) NOT NULL DEFAULT 'contact',
			organization VARCHAR(255) NULL,
			display_name VARCHAR(255) NULL,
			email VARCHAR(255) NULL,
			phone VARCHAR(64) NULL,
			target_source VARCHAR(64) NOT NULL DEFAULT 'suggested',
			confidence_score FLOAT NOT NULL DEFAULT 0,
			rationale TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_escalation_targets_case (case_id),
			KEY idx_case_escalation_targets_email (email),
			CONSTRAINT fk_case_escalation_targets_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_escalation_actions (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			target_id BIGINT UNSIGNED NULL,
			channel VARCHAR(32) NOT NULL DEFAULT 'email',
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			subject TEXT NULL,
			body TEXT NULL,
			attachments_json JSON NULL,
			sent_by_user_id VARCHAR(255) NULL,
			provider_message_id VARCHAR(255) NULL,
			sent_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_escalation_actions_case (case_id, created_at),
			KEY idx_case_escalation_actions_target (target_id),
			CONSTRAINT fk_case_escalation_actions_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
			CONSTRAINT fk_case_escalation_actions_target FOREIGN KEY (target_id) REFERENCES case_escalation_targets(id) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_email_deliveries (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			action_id BIGINT UNSIGNED NULL,
			target_id BIGINT UNSIGNED NULL,
			recipient_email VARCHAR(255) NOT NULL,
			delivery_status VARCHAR(32) NOT NULL DEFAULT 'sent',
			delivery_source VARCHAR(64) NOT NULL DEFAULT 'case_target',
			provider VARCHAR(32) NOT NULL DEFAULT 'sendgrid',
			provider_message_id VARCHAR(255) NULL,
			sent_at TIMESTAMP NULL,
			error_message TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_email_deliveries_case (case_id, created_at),
			KEY idx_case_email_deliveries_action (action_id),
			KEY idx_case_email_deliveries_target (target_id),
			KEY idx_case_email_deliveries_recipient (recipient_email),
			CONSTRAINT fk_case_email_deliveries_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
			CONSTRAINT fk_case_email_deliveries_action FOREIGN KEY (action_id) REFERENCES case_escalation_actions(id) ON DELETE SET NULL,
			CONSTRAINT fk_case_email_deliveries_target FOREIGN KEY (target_id) REFERENCES case_escalation_targets(id) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_resolution_signals (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			source_type VARCHAR(64) NOT NULL,
			summary TEXT NOT NULL,
			linked_report_seq INT NULL,
			metadata_json JSON NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_resolution_signals_case (case_id, created_at),
			KEY idx_case_resolution_signals_report (linked_report_seq),
			CONSTRAINT fk_case_resolution_signals_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
			CONSTRAINT fk_case_resolution_signals_report FOREIGN KEY (linked_report_seq) REFERENCES reports(seq) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_audit_events (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			event_type VARCHAR(64) NOT NULL,
			actor_user_id VARCHAR(255) NULL,
			payload_json JSON NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_audit_events_case (case_id, created_at),
			CONSTRAINT fk_case_audit_events_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure case table: %w", err)
		}
	}
	return nil
}

func ensureCaseEmailDeliveriesTable(ctx context.Context, db *sql.DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS case_email_deliveries (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		case_id VARCHAR(64) NOT NULL,
		action_id BIGINT UNSIGNED NULL,
		target_id BIGINT UNSIGNED NULL,
		recipient_email VARCHAR(255) NOT NULL,
		delivery_status VARCHAR(32) NOT NULL DEFAULT 'sent',
		delivery_source VARCHAR(64) NOT NULL DEFAULT 'case_target',
		provider VARCHAR(32) NOT NULL DEFAULT 'sendgrid',
		provider_message_id VARCHAR(255) NULL,
		sent_at TIMESTAMP NULL,
		error_message TEXT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		KEY idx_case_email_deliveries_case (case_id, created_at),
		KEY idx_case_email_deliveries_action (action_id),
		KEY idx_case_email_deliveries_target (target_id),
		KEY idx_case_email_deliveries_recipient (recipient_email),
		CONSTRAINT fk_case_email_deliveries_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE,
		CONSTRAINT fk_case_email_deliveries_action FOREIGN KEY (action_id) REFERENCES case_escalation_actions(id) ON DELETE SET NULL,
		CONSTRAINT fk_case_email_deliveries_target FOREIGN KEY (target_id) REFERENCES case_escalation_targets(id) ON DELETE SET NULL
	) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("failed to ensure case email deliveries table: %w", err)
	}
	return nil
}
