package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-ownership-service", []migrator.Step{
		{ID: "0001_reports_owners", Description: "create reports_owners table", Up: createReportsOwnersTable},
		{ID: "0002_reports_owners_is_public", Description: "ensure reports_owners visibility fields", Up: ensureReportsOwnersIsPublic},
	})
}

func createReportsOwnersTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS reports_owners (
			seq INT NOT NULL,
			owner VARCHAR(256) NOT NULL,
			is_public BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (seq, owner),
			INDEX idx_seq (seq),
			INDEX idx_is_public (is_public)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create reports_owners table: %w", err)
	}
	return nil
}

func ensureReportsOwnersIsPublic(ctx context.Context, db *sql.DB) error {
	var columnExists bool
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'reports_owners'
		AND COLUMN_NAME = 'is_public'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check if is_public column exists: %w", err)
	}
	if columnExists {
		return nil
	}
	_, err = db.ExecContext(ctx, `
		ALTER TABLE reports_owners
		ADD COLUMN is_public BOOLEAN DEFAULT FALSE,
		ADD INDEX idx_is_public (is_public)
	`)
	if err != nil {
		return fmt.Errorf("failed to add is_public column: %w", err)
	}
	return nil
}
