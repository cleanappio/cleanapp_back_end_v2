package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "gdpr-process-service", []migrator.Step{
		{ID: "0001_users_gdpr_table", Description: "create users_gdpr table", Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS users_gdpr(
					id VARCHAR(255) NOT NULL,
					processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
					PRIMARY KEY (id),
					UNIQUE INDEX id_unique (id)
				)`)
			if err != nil {
				return fmt.Errorf("failed to create users_gdpr table: %w", err)
			}
			return nil
		}},
		{ID: "0002_reports_gdpr_table", Description: "create reports_gdpr table", Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS reports_gdpr(
					seq INT NOT NULL,
					processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
					PRIMARY KEY (seq),
					UNIQUE INDEX seq_unique (seq)
				)`)
			if err != nil {
				return fmt.Errorf("failed to create reports_gdpr table: %w", err)
			}
			return nil
		}},
	})
}
