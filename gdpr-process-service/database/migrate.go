package database

import (
	"context"
	"database/sql"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "gdpr-process-service", []migrator.Step{
		{ID: "0001_gdpr_tables", Description: "create gdpr tracking tables", Up: func(ctx context.Context, db *sql.DB) error {
			return InitSchema(db)
		}},
	})
}
