package database

import (
	"context"
	"database/sql"

	"cleanapp-common/effectpolicy"
	"cleanapp-common/evidence"
	"cleanapp-common/migrator"
	"cleanapp-common/reportoutcome"
	"report-analyze-pipeline/contacts"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "report-analyze-pipeline", []migrator.Step{
		{ID: "0001_report_analysis_table", Description: "create report_analysis table", Up: createReportAnalysisTable},
		{ID: "0002_report_analysis_columns_and_indexes", Description: "migrate report_analysis columns and indexes", Up: migrateReportAnalysisTable},
		{ID: "0003_osm_cache_table", Description: "create osm cache table", Up: createOSMCacheTable},
		{ID: "0004_brand_contacts_table", Description: "create brand contacts table", Up: createBrandContactsTable},
		{ID: "0005_seed_known_contacts", Description: "seed known brand contacts", Up: seedKnownContacts},
		{ID: "0006_report_evidence", Description: "create per-report original-evidence storage", Up: evidence.Migrate},
		// Environments that applied the former column-based 0006 still need the
		// separate table. The shared create-only migration is safe to repeat.
		{ID: "0007_report_evidence_store", Description: "ensure separate per-report evidence storage", Up: evidence.Migrate},
		{ID: "0008_report_side_effect_policy", Description: "separate historical reports from outbound effects", Up: effectpolicy.Migrate},
		{ID: "0009_report_outcomes", Description: "persist agent decisions and publication recovery", Up: reportoutcome.Migrate},
	})
}

func seedKnownContacts(ctx context.Context, db *sql.DB) error {
	if err := contacts.NewContactService(db).SeedKnownContacts(); err != nil {
		return err
	}
	return nil
}
