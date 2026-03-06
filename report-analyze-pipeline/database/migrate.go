package database

import (
	"context"
	"database/sql"

	"cleanapp-common/migrator"
	"report-analyze-pipeline/contacts"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-analyze-pipeline", []migrator.Step{
		{ID: "0001_report_analysis_table", Description: "create report_analysis table", Up: createReportAnalysisTable},
		{ID: "0002_report_analysis_columns_and_indexes", Description: "migrate report_analysis columns and indexes", Up: migrateReportAnalysisTable},
		{ID: "0003_osm_cache_table", Description: "create osm cache table", Up: createOSMCacheTable},
		{ID: "0004_brand_contacts_table", Description: "create brand contacts table", Up: createBrandContactsTable},
		{ID: "0005_seed_known_contacts", Description: "seed known brand contacts", Up: seedKnownContacts},
	})
}

func seedKnownContacts(ctx context.Context, db *sql.DB) error {
	if err := contacts.NewContactService(db).SeedKnownContacts(); err != nil {
		return err
	}
	return nil
}
