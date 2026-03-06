package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
	"report-analyze-pipeline/contacts"
	"report-analyze-pipeline/osm"
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

func createReportAnalysisTable(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).CreateReportAnalysisTable()
}

func migrateReportAnalysisTable(ctx context.Context, db *sql.DB) error {
	return (&Database{db: db}).MigrateReportAnalysisTable()
}

func createOSMCacheTable(ctx context.Context, db *sql.DB) error {
	return osm.NewCachedLocationService(db).CreateCacheTable()
}

func createBrandContactsTable(ctx context.Context, db *sql.DB) error {
	return contacts.NewContactService(db).CreateBrandContactsTable()
}

func seedKnownContacts(ctx context.Context, db *sql.DB) error {
	if err := contacts.NewContactService(db).SeedKnownContacts(); err != nil {
		return fmt.Errorf("failed to seed known contacts: %w", err)
	}
	return nil
}
