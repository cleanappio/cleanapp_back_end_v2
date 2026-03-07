package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
