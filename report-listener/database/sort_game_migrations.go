package database

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureReportSortTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS report_sort_metrics (
			report_seq INT NOT NULL,
			sort_count INT NOT NULL DEFAULT 0,
			high_value_count INT NOT NULL DEFAULT 0,
			spam_count INT NOT NULL DEFAULT 0,
			urgency_sum INT NOT NULL DEFAULT 0,
			urgency_mean DECIMAL(10,4) NOT NULL DEFAULT 0.0000,
			last_sorted_at TIMESTAMP NULL DEFAULT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (report_seq),
			KEY idx_report_sort_metrics_last_sorted (last_sorted_at),
			CONSTRAINT fk_report_sort_metrics_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`); err != nil {
		return fmt.Errorf("failed to create report_sort_metrics table: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS report_sort_events (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			report_seq INT NOT NULL,
			sorter_id VARCHAR(255) NOT NULL,
			verdict ENUM('high_value', 'spam') NOT NULL,
			urgency_score TINYINT UNSIGNED NOT NULL DEFAULT 0,
			reward_kitns INT NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_report_sort_event_report_sorter (report_seq, sorter_id),
			KEY idx_report_sort_events_sorter (sorter_id, created_at),
			KEY idx_report_sort_events_verdict (verdict, created_at),
			CONSTRAINT fk_report_sort_events_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`); err != nil {
		return fmt.Errorf("failed to create report_sort_events table: %w", err)
	}

	return nil
}
