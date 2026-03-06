package database

import (
	"context"
	"database/sql"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-processor", []migrator.Step{
		{ID: "0001_report_status", Description: "create report_status table", Up: ensureReportStatusTableStep},
		{ID: "0002_responses", Description: "create responses table", Up: ensureResponsesTableStep},
		{ID: "0003_report_clusters", Description: "create report_clusters table", Up: ensureReportClustersTableStep},
	})
}

func ensureReportStatusTableStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureReportStatusTable(ctx)
}

func ensureResponsesTableStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureResponsesTable(ctx)
}

func ensureReportClustersTableStep(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).EnsureReportClustersTable(ctx)
}
