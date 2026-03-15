package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	mysqlDriver "github.com/go-sql-driver/mysql"

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

	lastSeq := 0
	for {
		filled, nextSeq, err := backfillReportPublicIDs(ctx, db, lastSeq, 50000)
		if err != nil {
			return err
		}
		if filled > 0 {
			log.Printf("reports.public_id migration progress: filled=%d through_seq=%d", filled, nextSeq)
		}
		if filled == 0 {
			break
		}
		lastSeq = nextSeq
	}

	for {
		cleared, err := clearDuplicateReportPublicIDs(ctx, db)
		if err != nil {
			return err
		}
		if cleared > 0 {
			log.Printf("reports.public_id duplicate cleanup progress: cleared=%d", cleared)
		}
		if cleared == 0 {
			break
		}
	}

	indexReady, err := indexExists(ctx, db, "reports", "uq_reports_public_id")
	if err != nil {
		return fmt.Errorf("failed to check reports.public_id index: %w", err)
	}
	if !indexReady {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE reports
			ALGORITHM=INPLACE,
			LOCK=NONE,
			ADD UNIQUE INDEX uq_reports_public_id (public_id)
		`); err != nil {
			return fmt.Errorf("failed to add reports.public_id unique index: %w", err)
		}
	}

	notNull, err := columnIsNotNull(ctx, db, "reports", "public_id")
	if err != nil {
		return fmt.Errorf("failed to check reports.public_id nullability: %w", err)
	}
	if !notNull {
		if err := enforceReportsPublicIDWrites(ctx, db); err != nil {
			return err
		}
	}

	return nil
}

func columnIsNotNull(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	var nullable string
	err := db.QueryRowContext(ctx, `
		SELECT IS_NULLABLE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = ?
		AND COLUMN_NAME = ?`,
		tableName, columnName,
	).Scan(&nullable)
	if err != nil {
		return false, fmt.Errorf("failed to load column nullability: %w", err)
	}
	return nullable == "NO", nil
}

func enforceReportsPublicIDWrites(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE reports
		ALGORITHM=INPLACE,
		LOCK=NONE,
		MODIFY COLUMN public_id VARCHAR(32) NOT NULL
	`); err == nil {
		return nil
	} else {
		var mysqlErr *mysqlDriver.MySQLError
		if !errors.As(err, &mysqlErr) || (mysqlErr.Number != 1845 && mysqlErr.Number != 1114) {
			return fmt.Errorf("failed to enforce reports.public_id NOT NULL: %w", err)
		}
		log.Printf("warn: reports.public_id NOT NULL rewrite skipped on existing table: %v", err)
	}

	if err := ensureReportPublicIDTrigger(ctx, db,
		"reports_public_id_before_insert",
		`CREATE TRIGGER reports_public_id_before_insert
		BEFORE INSERT ON reports
		FOR EACH ROW
		SET NEW.public_id = IF(
			NEW.public_id IS NULL OR NEW.public_id = '',
			CONCAT(
				'rpt_',
				REPLACE(
					REPLACE(
						REPLACE(TO_BASE64(RANDOM_BYTES(16)), '+', '-'),
						'/',
						'_'
					),
					'=',
					''
				)
			),
			NEW.public_id
		)`); err != nil {
		return err
	}

	if err := ensureReportPublicIDTrigger(ctx, db,
		"reports_public_id_before_update",
		`CREATE TRIGGER reports_public_id_before_update
		BEFORE UPDATE ON reports
		FOR EACH ROW
		SET NEW.public_id = IF(
			NEW.public_id IS NULL OR NEW.public_id = '' OR NEW.public_id <> OLD.public_id,
			OLD.public_id,
			NEW.public_id
		)`); err != nil {
		return err
	}

	return nil
}

func ensureReportPublicIDTrigger(ctx context.Context, db *sql.DB, triggerName, ddl string) error {
	exists, err := triggerExists(ctx, db, triggerName)
	if err != nil {
		return fmt.Errorf("failed to check trigger %s: %w", triggerName, err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to create trigger %s: %w", triggerName, err)
	}
	return nil
}

func triggerExists(ctx context.Context, db *sql.DB, triggerName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.TRIGGERS
		WHERE TRIGGER_SCHEMA = DATABASE()
		AND TRIGGER_NAME = ?`,
		triggerName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check trigger existence: %w", err)
	}
	return count > 0, nil
}

func backfillReportPublicIDs(ctx context.Context, db *sql.DB, afterSeq int, batchSize int) (int, int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq
		FROM reports
		WHERE seq > ? AND (public_id IS NULL OR public_id = '')
		ORDER BY seq ASC
		LIMIT ?
	`, afterSeq, batchSize)
	if err != nil {
		return 0, afterSeq, fmt.Errorf("failed to load report public_id backfill range: %w", err)
	}
	defer rows.Close()

	firstSeq := 0
	lastSeq := afterSeq
	selected := 0
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return 0, afterSeq, fmt.Errorf("failed to scan report seq for public_id backfill: %w", err)
		}
		if selected == 0 {
			firstSeq = seq
		}
		lastSeq = seq
		selected++
	}
	if err := rows.Err(); err != nil {
		return 0, afterSeq, fmt.Errorf("failed iterating report public_id backfill range: %w", err)
	}
	if selected == 0 {
		return 0, afterSeq, nil
	}

	// MySQL evaluates RANDOM_BYTES() per row here, so we can fill large batches
	// without shipping millions of generated IDs from the app layer.
	result, err := db.ExecContext(ctx, `
		UPDATE reports
		SET public_id = CONCAT(
			'rpt_',
			REPLACE(
				REPLACE(
					REPLACE(TO_BASE64(RANDOM_BYTES(16)), '+', '-'),
					'/',
					'_'
				),
				'=',
				''
			)
		)
		WHERE seq >= ? AND seq <= ? AND (public_id IS NULL OR public_id = '')
	`, firstSeq, lastSeq)
	if err != nil {
		return 0, afterSeq, fmt.Errorf("failed to backfill report public_id values: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, afterSeq, fmt.Errorf("failed to read backfilled report count: %w", err)
	}
	return int(affected), lastSeq, nil
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
			actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',
			channel VARCHAR(32) NOT NULL DEFAULT 'wire',
			auth_method VARCHAR(32) NOT NULL DEFAULT 'api_key',
			source_id VARCHAR(255) NOT NULL,
			schema_version VARCHAR(32) NOT NULL,
			submitted_at TIMESTAMP NOT NULL,
			observed_at TIMESTAMP NULL,
			agent_id VARCHAR(128) NOT NULL,
			lane VARCHAR(16) NOT NULL,
			material_hash CHAR(64) NOT NULL,
			submission_quality FLOAT NOT NULL DEFAULT 0,
			risk_score FLOAT NOT NULL DEFAULT 0,
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

func ensureWireSubmissionActorColumns(ctx context.Context, db *sql.DB) error {
	type columnSpec struct {
		name string
		sql  string
	}
	columns := []columnSpec{
		{
			name: "actor_kind",
			sql:  `ALTER TABLE wire_submissions_raw ADD COLUMN actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine' AFTER key_id`,
		},
		{
			name: "channel",
			sql:  `ALTER TABLE wire_submissions_raw ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'wire' AFTER actor_kind`,
		},
		{
			name: "auth_method",
			sql:  `ALTER TABLE wire_submissions_raw ADD COLUMN auth_method VARCHAR(32) NOT NULL DEFAULT 'api_key' AFTER channel`,
		},
		{
			name: "risk_score",
			sql:  `ALTER TABLE wire_submissions_raw ADD COLUMN risk_score FLOAT NOT NULL DEFAULT 0 AFTER submission_quality`,
		},
	}
	for _, column := range columns {
		exists, err := columnExists(ctx, db, "wire_submissions_raw", column.name)
		if err != nil {
			return fmt.Errorf("failed to check wire_submissions_raw.%s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add wire_submissions_raw.%s: %w", column.name, err)
		}
	}
	indexExists, err := indexExists(ctx, db, "wire_submissions_raw", "idx_wire_actor_channel_created")
	if err != nil {
		return fmt.Errorf("failed to check wire submission actor index: %w", err)
	}
	if !indexExists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE wire_submissions_raw
			ADD INDEX idx_wire_actor_channel_created (actor_kind, channel, created_at)
		`); err != nil {
			return fmt.Errorf("failed to create wire actor/channel index: %w", err)
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
			channel VARCHAR(32) NOT NULL DEFAULT 'email',
			email VARCHAR(255) NULL,
			phone VARCHAR(64) NULL,
			website VARCHAR(512) NULL,
			contact_url VARCHAR(512) NULL,
			social_platform VARCHAR(64) NULL,
			social_handle VARCHAR(255) NULL,
			source_url VARCHAR(512) NULL,
			evidence_text TEXT NULL,
			verification_level VARCHAR(64) NULL,
			target_source VARCHAR(64) NOT NULL DEFAULT 'suggested',
			confidence_score FLOAT NOT NULL DEFAULT 0,
			rationale TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_escalation_targets_case (case_id),
			KEY idx_case_escalation_targets_email (email),
			KEY idx_case_escalation_targets_channel (case_id, channel),
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

func ensureCaseEscalationTargetChannels(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "channel", sql: "ALTER TABLE case_escalation_targets ADD COLUMN channel VARCHAR(32) NOT NULL DEFAULT 'email' AFTER display_name"},
		{name: "website", sql: "ALTER TABLE case_escalation_targets ADD COLUMN website VARCHAR(512) NULL AFTER phone"},
		{name: "contact_url", sql: "ALTER TABLE case_escalation_targets ADD COLUMN contact_url VARCHAR(512) NULL AFTER website"},
		{name: "social_platform", sql: "ALTER TABLE case_escalation_targets ADD COLUMN social_platform VARCHAR(64) NULL AFTER contact_url"},
		{name: "social_handle", sql: "ALTER TABLE case_escalation_targets ADD COLUMN social_handle VARCHAR(255) NULL AFTER social_platform"},
	}
	for _, column := range columns {
		exists, err := columnExists(ctx, db, "case_escalation_targets", column.name)
		if err != nil {
			return fmt.Errorf("failed to check case_escalation_targets.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add case_escalation_targets.%s column: %w", column.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE case_escalation_targets
		SET channel = CASE
			WHEN COALESCE(email, '') <> '' THEN 'email'
			WHEN COALESCE(phone, '') <> '' THEN 'phone'
			ELSE 'website'
		END
		WHERE COALESCE(channel, '') = '' OR channel = 'email'
	`); err != nil {
		return fmt.Errorf("failed to backfill case escalation target channels: %w", err)
	}

	exists, err := indexExists(ctx, db, "case_escalation_targets", "idx_case_escalation_targets_channel")
	if err != nil {
		return fmt.Errorf("failed to check case escalation target channel index: %w", err)
	}
	if !exists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE case_escalation_targets
			ADD INDEX idx_case_escalation_targets_channel (case_id, channel)
		`); err != nil {
			return fmt.Errorf("failed to add case escalation target channel index: %w", err)
		}
	}

	return nil
}

func ensureCaseEscalationTargetEvidenceColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "source_url", sql: "ALTER TABLE case_escalation_targets ADD COLUMN source_url VARCHAR(512) NULL AFTER social_handle"},
		{name: "evidence_text", sql: "ALTER TABLE case_escalation_targets ADD COLUMN evidence_text TEXT NULL AFTER source_url"},
		{name: "verification_level", sql: "ALTER TABLE case_escalation_targets ADD COLUMN verification_level VARCHAR(64) NULL AFTER evidence_text"},
	}
	for _, column := range columns {
		exists, err := columnExists(ctx, db, "case_escalation_targets", column.name)
		if err != nil {
			return fmt.Errorf("failed to check case_escalation_targets.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add case_escalation_targets.%s column: %w", column.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE case_escalation_targets
		SET
			source_url = CASE
				WHEN COALESCE(source_url, '') <> '' THEN source_url
				WHEN COALESCE(contact_url, '') <> '' THEN contact_url
				WHEN COALESCE(website, '') <> '' THEN website
				ELSE NULL
			END,
			verification_level = CASE
				WHEN COALESCE(verification_level, '') <> '' THEN verification_level
				WHEN target_source LIKE 'area_contact%' THEN 'mapped_area_contact'
				WHEN target_source LIKE 'osm_%' THEN 'openstreetmap'
				WHEN target_source LIKE 'google_places%' THEN 'directory_listing'
				WHEN target_source LIKE 'web_search%' THEN 'web_search_result'
				WHEN target_source = 'inferred_contact' THEN 'inferred'
				ELSE 'discovered'
			END
		WHERE COALESCE(source_url, '') = '' OR COALESCE(verification_level, '') = ''
	`); err != nil {
		return fmt.Errorf("failed to backfill case escalation target evidence columns: %w", err)
	}

	return nil
}

func ensureCaseContactRoutingTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS case_contact_observations (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			role_type VARCHAR(64) NOT NULL DEFAULT 'contact',
			decision_scope VARCHAR(64) NOT NULL DEFAULT 'other',
			organization_name VARCHAR(255) NULL,
			person_name VARCHAR(255) NULL,
			channel_type VARCHAR(32) NOT NULL DEFAULT 'email',
			channel_value VARCHAR(512) NULL,
			email VARCHAR(255) NULL,
			phone VARCHAR(64) NULL,
			website VARCHAR(512) NULL,
			contact_url VARCHAR(512) NULL,
			social_platform VARCHAR(64) NULL,
			social_handle VARCHAR(255) NULL,
			source_url VARCHAR(512) NULL,
			evidence_text TEXT NULL,
			verification_level VARCHAR(64) NULL,
			attribution_class VARCHAR(64) NOT NULL DEFAULT 'heuristic',
			confidence_score FLOAT NOT NULL DEFAULT 0,
			target_source VARCHAR(64) NOT NULL DEFAULT 'suggested',
			discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_contact_observations_case (case_id, confidence_score),
			KEY idx_case_contact_observations_scope (case_id, decision_scope),
			KEY idx_case_contact_observations_channel (case_id, channel_type),
			CONSTRAINT fk_case_contact_observations_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_notify_plans (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			case_id VARCHAR(64) NOT NULL,
			plan_version INT NOT NULL DEFAULT 1,
			hazard_mode VARCHAR(64) NOT NULL DEFAULT 'standard',
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			summary TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_notify_plans_case (case_id, status, updated_at),
			CONSTRAINT fk_case_notify_plans_case FOREIGN KEY (case_id) REFERENCES cases(case_id) ON DELETE CASCADE
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS case_notify_plan_items (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			plan_id BIGINT UNSIGNED NOT NULL,
			target_id BIGINT UNSIGNED NULL,
			observation_id BIGINT UNSIGNED NULL,
			wave_number INT NOT NULL DEFAULT 1,
			priority_rank INT NOT NULL DEFAULT 0,
			role_type VARCHAR(64) NOT NULL DEFAULT 'contact',
			decision_scope VARCHAR(64) NOT NULL DEFAULT 'other',
			actionability_score FLOAT NOT NULL DEFAULT 0,
			send_eligibility VARCHAR(32) NOT NULL DEFAULT 'review',
			reason_selected TEXT NULL,
			selected BOOL NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_case_notify_plan_items_plan (plan_id, wave_number, priority_rank),
			KEY idx_case_notify_plan_items_target (target_id),
			KEY idx_case_notify_plan_items_observation (observation_id),
			CONSTRAINT fk_case_notify_plan_items_plan FOREIGN KEY (plan_id) REFERENCES case_notify_plans(id) ON DELETE CASCADE,
			CONSTRAINT fk_case_notify_plan_items_target FOREIGN KEY (target_id) REFERENCES case_escalation_targets(id) ON DELETE SET NULL,
			CONSTRAINT fk_case_notify_plan_items_observation FOREIGN KEY (observation_id) REFERENCES case_contact_observations(id) ON DELETE SET NULL
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure case contact routing table: %w", err)
		}
	}

	targetColumns := []struct {
		name string
		sql  string
	}{
		{name: "decision_scope", sql: "ALTER TABLE case_escalation_targets ADD COLUMN decision_scope VARCHAR(64) NOT NULL DEFAULT 'other' AFTER role_type"},
		{name: "attribution_class", sql: "ALTER TABLE case_escalation_targets ADD COLUMN attribution_class VARCHAR(64) NOT NULL DEFAULT 'heuristic' AFTER verification_level"},
		{name: "actionability_score", sql: "ALTER TABLE case_escalation_targets ADD COLUMN actionability_score FLOAT NOT NULL DEFAULT 0 AFTER confidence_score"},
		{name: "notify_tier", sql: "ALTER TABLE case_escalation_targets ADD COLUMN notify_tier INT NOT NULL DEFAULT 3 AFTER actionability_score"},
		{name: "send_eligibility", sql: "ALTER TABLE case_escalation_targets ADD COLUMN send_eligibility VARCHAR(32) NOT NULL DEFAULT 'review' AFTER notify_tier"},
		{name: "reason_selected", sql: "ALTER TABLE case_escalation_targets ADD COLUMN reason_selected TEXT NULL AFTER rationale"},
	}
	for _, column := range targetColumns {
		exists, err := columnExists(ctx, db, "case_escalation_targets", column.name)
		if err != nil {
			return fmt.Errorf("failed to check case_escalation_targets.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add case_escalation_targets.%s column: %w", column.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE case_escalation_targets
		SET
			decision_scope = CASE
				WHEN role_type IN ('operator', 'operator_admin', 'site_leadership', 'facility_manager') THEN 'site_ops'
				WHEN role_type IN ('owner', 'property_owner', 'landlord') THEN 'asset_owner'
				WHEN role_type IN ('building_authority', 'public_safety', 'fire_authority') THEN 'regulator'
				WHEN role_type IN ('architect', 'engineer', 'contractor') THEN 'project_party'
				ELSE 'other'
			END,
			attribution_class = CASE
				WHEN COALESCE(verification_level, '') LIKE 'official%' OR target_source LIKE 'area_contact%' THEN 'official_direct'
				WHEN target_source LIKE 'google_places%' OR COALESCE(verification_level, '') IN ('directory_listing', 'mapped_area_contact') THEN 'official_registry'
				WHEN target_source = 'inferred_contact' THEN 'heuristic'
				ELSE 'heuristic'
			END
	`); err != nil {
		return fmt.Errorf("failed to backfill case contact routing columns: %w", err)
	}

	return nil
}

func ensureCaseAccumulationColumns(ctx context.Context, db *sql.DB) error {
	savedClusterColumns := []struct {
		name string
		sql  string
	}{
		{name: "bbox_json", sql: "ALTER TABLE saved_clusters ADD COLUMN bbox_json JSON NULL AFTER geometry_json"},
		{name: "centroid_lat", sql: "ALTER TABLE saved_clusters ADD COLUMN centroid_lat DOUBLE NULL AFTER bbox_json"},
		{name: "centroid_lng", sql: "ALTER TABLE saved_clusters ADD COLUMN centroid_lng DOUBLE NULL AFTER centroid_lat"},
		{name: "cluster_fingerprint", sql: "ALTER TABLE saved_clusters ADD COLUMN cluster_fingerprint VARCHAR(64) NOT NULL DEFAULT '' AFTER centroid_lng"},
	}
	for _, column := range savedClusterColumns {
		exists, err := columnExists(ctx, db, "saved_clusters", column.name)
		if err != nil {
			return fmt.Errorf("failed to check saved_clusters.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add saved_clusters.%s column: %w", column.name, err)
		}
	}

	caseColumns := []struct {
		name string
		sql  string
	}{
		{name: "aggregate_geometry_json", sql: "ALTER TABLE cases ADD COLUMN aggregate_geometry_json JSON NULL AFTER geometry_json"},
		{name: "aggregate_bbox_json", sql: "ALTER TABLE cases ADD COLUMN aggregate_bbox_json JSON NULL AFTER aggregate_geometry_json"},
		{name: "cluster_count", sql: "ALTER TABLE cases ADD COLUMN cluster_count INT NOT NULL DEFAULT 0 AFTER trend_score"},
		{name: "linked_report_count", sql: "ALTER TABLE cases ADD COLUMN linked_report_count INT NOT NULL DEFAULT 0 AFTER cluster_count"},
		{name: "last_cluster_at", sql: "ALTER TABLE cases ADD COLUMN last_cluster_at TIMESTAMP NULL AFTER last_seen_at"},
		{name: "merged_into_case_id", sql: "ALTER TABLE cases ADD COLUMN merged_into_case_id VARCHAR(64) NULL AFTER last_cluster_at"},
	}
	for _, column := range caseColumns {
		exists, err := columnExists(ctx, db, "cases", column.name)
		if err != nil {
			return fmt.Errorf("failed to check cases.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add cases.%s column: %w", column.name, err)
		}
	}

	caseClusterColumns := []struct {
		name string
		sql  string
	}{
		{name: "match_score", sql: "ALTER TABLE case_clusters ADD COLUMN match_score FLOAT NOT NULL DEFAULT 0 AFTER cluster_id"},
		{name: "match_reason", sql: "ALTER TABLE case_clusters ADD COLUMN match_reason TEXT NULL AFTER match_score"},
	}
	for _, column := range caseClusterColumns {
		exists, err := columnExists(ctx, db, "case_clusters", column.name)
		if err != nil {
			return fmt.Errorf("failed to check case_clusters.%s column: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("failed to add case_clusters.%s column: %w", column.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE cases c
		LEFT JOIN (
			SELECT case_id, COUNT(*) AS cluster_count, MAX(linked_at) AS last_cluster_at
			FROM case_clusters
			GROUP BY case_id
		) cc ON cc.case_id = c.case_id
		LEFT JOIN (
			SELECT case_id, COUNT(*) AS linked_report_count
			FROM case_reports
			GROUP BY case_id
		) cr ON cr.case_id = c.case_id
		SET
			c.cluster_count = COALESCE(cc.cluster_count, 0),
			c.linked_report_count = COALESCE(cr.linked_report_count, 0),
			c.last_cluster_at = cc.last_cluster_at
	`); err != nil {
		return fmt.Errorf("failed to backfill case accumulation counts: %w", err)
	}

	mergedIdxExists, err := indexExists(ctx, db, "cases", "idx_cases_merged_into")
	if err != nil {
		return fmt.Errorf("failed to check cases merged index: %w", err)
	}
	if !mergedIdxExists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE cases
			ADD INDEX idx_cases_merged_into (merged_into_case_id)
		`); err != nil {
			return fmt.Errorf("failed to add merged_into_case_id index: %w", err)
		}
	}

	lastClusterIdxExists, err := indexExists(ctx, db, "cases", "idx_cases_status_last_cluster")
	if err != nil {
		return fmt.Errorf("failed to check cases status/last_cluster index: %w", err)
	}
	if !lastClusterIdxExists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE cases
			ADD INDEX idx_cases_status_last_cluster (status, classification, last_cluster_at)
		`); err != nil {
			return fmt.Errorf("failed to add status/classification/last_cluster index: %w", err)
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

func ensureReportContactTargetTables(ctx context.Context, db *sql.DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS report_escalation_targets (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		report_seq INT NOT NULL,
		role_type VARCHAR(64) NOT NULL DEFAULT 'contact',
		decision_scope VARCHAR(64) NOT NULL DEFAULT 'other',
		organization VARCHAR(255) NULL,
		display_name VARCHAR(255) NULL,
		channel VARCHAR(32) NOT NULL DEFAULT 'email',
		email VARCHAR(255) NULL,
		phone VARCHAR(64) NULL,
		website VARCHAR(512) NULL,
		contact_url VARCHAR(512) NULL,
		social_platform VARCHAR(64) NULL,
		social_handle VARCHAR(255) NULL,
		source_url VARCHAR(512) NULL,
		evidence_text TEXT NULL,
		verification_level VARCHAR(64) NULL,
		attribution_class VARCHAR(64) NOT NULL DEFAULT 'heuristic',
		target_source VARCHAR(64) NOT NULL DEFAULT 'suggested',
		confidence_score FLOAT NOT NULL DEFAULT 0,
		actionability_score FLOAT NOT NULL DEFAULT 0,
		notify_tier INT NOT NULL DEFAULT 3,
		send_eligibility VARCHAR(32) NOT NULL DEFAULT 'review',
		rationale TEXT NULL,
		reason_selected TEXT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		KEY idx_report_escalation_targets_report (report_seq, notify_tier, actionability_score),
		KEY idx_report_escalation_targets_email (email),
		KEY idx_report_escalation_targets_channel (report_seq, channel),
		CONSTRAINT fk_report_escalation_targets_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
	) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("failed to ensure report escalation targets table: %w", err)
	}
	return nil
}

func ensureUnifiedDefectRoutingTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS subject_routing_profiles (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			subject_kind VARCHAR(16) NOT NULL,
			subject_ref VARCHAR(128) NOT NULL,
			classification VARCHAR(32) NOT NULL DEFAULT 'physical',
			defect_class VARCHAR(64) NOT NULL DEFAULT 'general_defect',
			defect_mode VARCHAR(64) NOT NULL DEFAULT 'standard',
			asset_class VARCHAR(64) NOT NULL DEFAULT 'general_site',
			jurisdiction_key VARCHAR(128) NOT NULL DEFAULT '',
			exposure_mode VARCHAR(64) NOT NULL DEFAULT 'localized',
			severity_band VARCHAR(32) NOT NULL DEFAULT 'medium',
			urgency_band VARCHAR(32) NOT NULL DEFAULT 'medium',
			context_json JSON NULL,
			refreshed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_subject_routing_profile (subject_kind, subject_ref),
			KEY idx_subject_routing_profile_lookup (subject_kind, classification, defect_class, asset_class)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS contact_endpoint_memory (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			endpoint_key VARCHAR(255) NOT NULL,
			organization_key VARCHAR(255) NOT NULL DEFAULT '',
			channel_type VARCHAR(32) NOT NULL DEFAULT 'email',
			channel_value VARCHAR(512) NOT NULL,
			last_result VARCHAR(32) NOT NULL DEFAULT '',
			success_count INT NOT NULL DEFAULT 0,
			bounce_count INT NOT NULL DEFAULT 0,
			ack_count INT NOT NULL DEFAULT 0,
			fix_count INT NOT NULL DEFAULT 0,
			misroute_count INT NOT NULL DEFAULT 0,
			no_response_count INT NOT NULL DEFAULT 0,
			last_contacted_at TIMESTAMP NULL,
			cooldown_until TIMESTAMP NULL,
			preferred_for_role_type VARCHAR(64) NOT NULL DEFAULT '',
			preferred_for_asset_class VARCHAR(64) NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_contact_endpoint_memory (endpoint_key),
			KEY idx_contact_endpoint_memory_org (organization_key, channel_type),
			KEY idx_contact_endpoint_memory_role (preferred_for_role_type, preferred_for_asset_class)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS notify_execution_tasks (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			subject_kind VARCHAR(16) NOT NULL,
			subject_ref VARCHAR(128) NOT NULL,
			target_id BIGINT UNSIGNED NULL,
			wave_number INT NOT NULL DEFAULT 1,
			role_type VARCHAR(64) NOT NULL DEFAULT 'contact',
			channel_type VARCHAR(32) NOT NULL DEFAULT 'email',
			execution_mode VARCHAR(32) NOT NULL DEFAULT 'review',
			task_status VARCHAR(32) NOT NULL DEFAULT 'pending',
			summary VARCHAR(255) NOT NULL DEFAULT '',
			payload_json JSON NULL,
			assigned_user_id VARCHAR(255) NULL,
			due_at TIMESTAMP NULL,
			completed_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_notify_execution_tasks_subject (subject_kind, subject_ref, task_status, wave_number),
			KEY idx_notify_execution_tasks_target (target_id)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS notify_outcomes (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			subject_kind VARCHAR(16) NOT NULL,
			subject_ref VARCHAR(128) NOT NULL,
			target_id BIGINT UNSIGNED NULL,
			endpoint_key VARCHAR(255) NOT NULL DEFAULT '',
			outcome_type VARCHAR(32) NOT NULL,
			source_type VARCHAR(32) NOT NULL DEFAULT '',
			source_ref VARCHAR(255) NOT NULL DEFAULT '',
			evidence_json JSON NULL,
			recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_notify_outcomes_subject (subject_kind, subject_ref, recorded_at),
			KEY idx_notify_outcomes_endpoint (endpoint_key, outcome_type, recorded_at),
			KEY idx_notify_outcomes_target (target_id)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS authority_directory_rules (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			jurisdiction_key VARCHAR(128) NOT NULL DEFAULT '*',
			asset_class VARCHAR(64) NOT NULL DEFAULT 'general_site',
			defect_class VARCHAR(64) NOT NULL DEFAULT 'general_defect',
			role_type VARCHAR(64) NOT NULL,
			query_templates_json JSON NULL,
			official_domains_json JSON NULL,
			priority INT NOT NULL DEFAULT 100,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_authority_directory_rule (jurisdiction_key, asset_class, defect_class, role_type),
			KEY idx_authority_directory_rule_lookup (jurisdiction_key, asset_class, defect_class, priority)
		) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure unified defect routing table: %w", err)
		}
	}

	for _, table := range []string{"case_escalation_targets", "report_escalation_targets"} {
		columns := []struct {
			name string
			sql  string
		}{
			{name: "endpoint_key", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN endpoint_key VARCHAR(255) NOT NULL DEFAULT '' AFTER decision_scope", table)},
			{name: "organization_key", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN organization_key VARCHAR(255) NOT NULL DEFAULT '' AFTER endpoint_key", table)},
			{name: "site_match_score", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN site_match_score FLOAT NOT NULL DEFAULT 0 AFTER confidence_score", table)},
			{name: "source_quality_score", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN source_quality_score FLOAT NOT NULL DEFAULT 0 AFTER site_match_score", table)},
			{name: "role_fit_score", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN role_fit_score FLOAT NOT NULL DEFAULT 0 AFTER source_quality_score", table)},
			{name: "channel_quality_score", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN channel_quality_score FLOAT NOT NULL DEFAULT 0 AFTER role_fit_score", table)},
			{name: "outcome_memory_score", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN outcome_memory_score FLOAT NOT NULL DEFAULT 0 AFTER channel_quality_score", table)},
			{name: "execution_mode", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN execution_mode VARCHAR(32) NOT NULL DEFAULT 'review' AFTER send_eligibility", table)},
			{name: "cooldown_until", sql: fmt.Sprintf("ALTER TABLE %s ADD COLUMN cooldown_until TIMESTAMP NULL AFTER execution_mode", table)},
		}
		for _, column := range columns {
			exists, err := columnExists(ctx, db, table, column.name)
			if err != nil {
				return fmt.Errorf("failed to check %s.%s column: %w", table, column.name, err)
			}
			if exists {
				continue
			}
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("failed to add %s.%s column: %w", table, column.name, err)
			}
		}
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO authority_directory_rules (
			jurisdiction_key, asset_class, defect_class, role_type, query_templates_json, official_domains_json, priority
		) VALUES
			('*', 'school', 'physical_structural', 'building_authority', JSON_ARRAY('building department', 'building safety', 'planning office'), JSON_ARRAY(), 10),
			('*', 'school', 'physical_structural', 'public_safety', JSON_ARRAY('public safety', 'police', 'emergency management'), JSON_ARRAY(), 20),
			('*', 'transit_station', 'physical_structural', 'transit_authority', JSON_ARRAY('station authority', 'transit authority', 'operations'), JSON_ARRAY(), 10),
			('*', 'transit_station', 'physical_structural', 'transit_safety', JSON_ARRAY('rail safety', 'transit safety'), JSON_ARRAY(), 20),
			('*', 'roadway', 'physical_safety', 'public_works', JSON_ARRAY('public works', 'roads maintenance', 'highway maintenance'), JSON_ARRAY(), 10),
			('*', 'bridge', 'physical_structural', 'infrastructure_authority', JSON_ARRAY('bridge maintenance', 'infrastructure maintenance'), JSON_ARRAY(), 10),
			('*', 'general_site', 'digital_product_bug', 'support', JSON_ARRAY('support', 'contact', 'help center'), JSON_ARRAY(), 10),
			('*', 'general_site', 'digital_product_bug', 'engineering', JSON_ARRAY('engineering', 'product team', 'developer relations'), JSON_ARRAY(), 20),
			('*', 'general_site', 'digital_security', 'security', JSON_ARRAY('security', 'security contact', 'responsible disclosure'), JSON_ARRAY(), 10),
			('*', 'general_site', 'digital_security', 'trust_safety', JSON_ARRAY('trust and safety', 'abuse', 'platform integrity'), JSON_ARRAY(), 20),
			('*', 'general_site', 'digital_accessibility', 'support', JSON_ARRAY('accessibility', 'support', 'help center'), JSON_ARRAY(), 10),
			('*', 'general_site', 'digital_accessibility', 'product_owner', JSON_ARRAY('product', 'accessibility', 'engineering'), JSON_ARRAY(), 20)
		ON DUPLICATE KEY UPDATE
			query_templates_json = VALUES(query_templates_json),
			official_domains_json = VALUES(official_domains_json),
			priority = VALUES(priority),
			updated_at = CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("failed to seed authority directory rules: %w", err)
	}

	return nil
}

func ensureNotifyQualityTuning(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		INSERT INTO authority_directory_rules (
			jurisdiction_key, asset_class, defect_class, role_type, query_templates_json, official_domains_json, priority
		) VALUES
			('*', 'retail_site', 'physical_safety', 'facility_manager', JSON_ARRAY('facility management', 'store management', 'site contact'), JSON_ARRAY(), 15),
			('*', 'hospital', 'physical_structural', 'facility_manager', JSON_ARRAY('facilities', 'building management', 'site operations'), JSON_ARRAY(), 15),
			('*', 'hospital', 'physical_structural', 'building_authority', JSON_ARRAY('building department', 'life safety', 'inspection office'), JSON_ARRAY(), 20),
			('*', 'public_building', 'physical_structural', 'building_authority', JSON_ARRAY('building department', 'inspection office', 'planning office'), JSON_ARRAY(), 18),
			('*', 'public_building', 'physical_structural', 'fire_authority', JSON_ARRAY('fire marshal', 'fire prevention', 'life safety'), JSON_ARRAY(), 24),
			('*', 'industrial_site', 'physical_structural', 'facility_manager', JSON_ARRAY('plant operations', 'maintenance', 'site contact'), JSON_ARRAY(), 15),
			('*', 'industrial_site', 'physical_structural', 'public_safety', JSON_ARRAY('public safety', 'emergency management', 'hazmat'), JSON_ARRAY(), 24),
			('*', 'general_site', 'physical_sanitation', 'public_works', JSON_ARRAY('public works', 'sanitation department', 'waste management'), JSON_ARRAY(), 16),
			('*', 'general_site', 'physical_sanitation', 'operator', JSON_ARRAY('site management', 'facilities', 'contact'), JSON_ARRAY(), 20),
			('*', 'roadway', 'physical_accessibility', 'traffic_authority', JSON_ARRAY('traffic engineering', 'transportation department', 'accessibility office'), JSON_ARRAY(), 16),
			('*', 'general_site', 'physical_accessibility', 'operator', JSON_ARRAY('accessibility', 'site management', 'contact'), JSON_ARRAY(), 20),
			('*', 'general_site', 'digital_fraud', 'trust_safety', JSON_ARRAY('trust and safety', 'fraud', 'abuse'), JSON_ARRAY(), 10),
			('*', 'general_site', 'digital_fraud', 'support', JSON_ARRAY('support', 'fraud support', 'customer support'), JSON_ARRAY(), 20),
			('*', 'general_site', 'digital_fraud', 'security', JSON_ARRAY('security', 'report abuse', 'integrity'), JSON_ARRAY(), 30),
			('*', 'general_site', 'operational_service', 'operator', JSON_ARRAY('operations', 'contact', 'customer support'), JSON_ARRAY(), 12),
			('*', 'general_site', 'operational_service', 'support', JSON_ARRAY('support', 'service desk', 'help center'), JSON_ARRAY(), 18),
			('*', 'transit_station', 'physical_safety', 'transit_authority', JSON_ARRAY('station management', 'transit authority', 'operations'), JSON_ARRAY(), 12),
			('country:ch', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('bauamt', 'bau und planung', 'hochbau'), JSON_ARRAY('*.admin.ch', '*.ch'), 8),
			('country:ch', 'general_site', 'physical_structural', 'fire_authority', JSON_ARRAY('feuerpolizei', 'feuerwehr', 'brandschutz'), JSON_ARRAY('*.admin.ch', '*.ch'), 14),
			('country:ch', 'school', 'physical_structural', 'facility_manager', JSON_ARRAY('hausdienst', 'schulverwaltung', 'kontakt'), JSON_ARRAY('*.schule-*.ch', '*.schulen-*.ch', '*.ch'), 9),
			('country:ch', 'transit_station', 'physical_structural', 'transit_authority', JSON_ARRAY('verkehrsbetriebe', 'bahnhof kontakt', 'betriebsleitung'), JSON_ARRAY('*.ch', '*.admin.ch'), 10),
			('country:ch', 'transit_station', 'physical_structural', 'transit_safety', JSON_ARRAY('bahnsicherheit', 'verkehrssicherheit'), JSON_ARRAY('*.ch', '*.admin.ch'), 18),
			('country:ch', 'general_site', 'physical_sanitation', 'public_works', JSON_ARRAY('werkhof', 'tiefbauamt', 'entsorgung', 'abfall'), JSON_ARRAY('*.admin.ch', '*.ch'), 12),
			('country:us', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('building department', 'building safety', 'code enforcement'), JSON_ARRAY('*.gov', '*.us'), 8),
			('country:us', 'general_site', 'physical_structural', 'fire_authority', JSON_ARRAY('fire marshal', 'fire prevention', 'life safety'), JSON_ARRAY('*.gov', '*.us'), 14),
			('country:us', 'roadway', 'physical_safety', 'public_works', JSON_ARRAY('public works', 'street maintenance', 'road maintenance'), JSON_ARRAY('*.gov', '*.us'), 10),
			('country:us', 'roadway', 'physical_safety', 'traffic_authority', JSON_ARRAY('traffic engineering', 'transportation department', 'mobility division'), JSON_ARRAY('*.gov', '*.us'), 16),
			('country:us', 'bridge', 'physical_structural', 'infrastructure_authority', JSON_ARRAY('bridge maintenance', 'bridge division', 'infrastructure maintenance'), JSON_ARRAY('*.gov', '*.us'), 10),
			('country:us', 'transit_station', 'physical_structural', 'transit_authority', JSON_ARRAY('transit authority', 'station management', 'rail operations'), JSON_ARRAY('*.gov', '*.us', '*.org'), 10),
			('country:us', 'transit_station', 'physical_structural', 'transit_safety', JSON_ARRAY('rail safety', 'transit safety', 'system safety'), JSON_ARRAY('*.gov', '*.us', '*.org'), 18),
			('country:us', 'general_site', 'digital_security', 'security', JSON_ARRAY('security', 'responsible disclosure', 'security contact'), JSON_ARRAY(), 10),
			('country:us', 'general_site', 'digital_fraud', 'trust_safety', JSON_ARRAY('trust and safety', 'fraud', 'abuse'), JSON_ARRAY(), 12),
			('country:de', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('bauamt', 'bauaufsicht', 'bauordnung'), JSON_ARRAY('*.de'), 8),
			('country:de', 'general_site', 'physical_structural', 'fire_authority', JSON_ARRAY('feuerwehr', 'brandschutz', 'feuerpolizei'), JSON_ARRAY('*.de'), 14),
			('country:at', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('bauamt', 'baupolizei', 'bauwesen'), JSON_ARRAY('*.gv.at', '*.at'), 8),
			('country:fr', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('service urbanisme', 'bâtiments', 'sécurité bâtiment'), JSON_ARRAY('*.fr', '*.gouv.fr'), 8),
			('country:it', 'general_site', 'physical_structural', 'building_authority', JSON_ARRAY('ufficio tecnico', 'edilizia', 'sicurezza edificio'), JSON_ARRAY('*.it', '*.gov.it'), 8)
		ON DUPLICATE KEY UPDATE
			query_templates_json = VALUES(query_templates_json),
			official_domains_json = VALUES(official_domains_json),
			priority = VALUES(priority),
			updated_at = CURRENT_TIMESTAMP
	`); err != nil {
		return fmt.Errorf("failed to seed notify quality tuning authority rules: %w", err)
	}
	return nil
}

func ensureMobilePushDeliveryTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mobile_push_devices (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			install_id VARCHAR(191) NOT NULL,
			platform VARCHAR(32) NOT NULL,
			provider VARCHAR(32) NOT NULL,
			push_token TEXT NOT NULL,
			push_token_hash CHAR(64) NOT NULL,
			app_version VARCHAR(64) NOT NULL DEFAULT '',
			notifications_enabled BOOL NOT NULL DEFAULT TRUE,
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uq_mobile_push_device_token_hash (push_token_hash),
			UNIQUE KEY uq_mobile_push_install_provider (install_id, provider),
			INDEX idx_mobile_push_devices_install (install_id),
			INDEX idx_mobile_push_devices_status (status),
			INDEX idx_mobile_push_devices_last_seen (last_seen_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`); err != nil {
		return fmt.Errorf("failed to create mobile_push_devices table: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS report_push_subscriptions (
			report_seq INT NOT NULL,
			install_id VARCHAR(191) NOT NULL,
			notification_kind VARCHAR(32) NOT NULL DEFAULT 'delivery',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (report_seq, install_id, notification_kind),
			INDEX idx_report_push_subscriptions_install (install_id),
			INDEX idx_report_push_subscriptions_kind (notification_kind)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`); err != nil {
		return fmt.Errorf("failed to create report_push_subscriptions table: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mobile_push_delivery_events (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			report_seq INT NOT NULL,
			install_id VARCHAR(191) NOT NULL,
			delivery_status VARCHAR(32) NOT NULL,
			provider VARCHAR(32) NOT NULL DEFAULT '',
			response_code INT NULL,
			response_body TEXT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uq_mobile_push_delivery_event (report_seq, install_id, delivery_status),
			INDEX idx_mobile_push_delivery_events_report (report_seq),
			INDEX idx_mobile_push_delivery_events_install (install_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`); err != nil {
		return fmt.Errorf("failed to create mobile_push_delivery_events table: %w", err)
	}

	// Older deployments may have rows inserted before hashing logic existed.
	if _, err := db.ExecContext(ctx, `
		UPDATE mobile_push_devices
		SET push_token_hash = SHA2(push_token, 256)
		WHERE push_token_hash = '' OR push_token_hash IS NULL
	`); err != nil {
		return fmt.Errorf("failed to backfill mobile_push_devices.push_token_hash: %w", err)
	}

	return nil
}
