package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "areas-service", []migrator.Step{
		{ID: "0001_areas_table", Description: "create areas table", Up: createAreasTable},
		{ID: "0002_contact_emails_table", Description: "create contact_emails table", Up: createContactEmailsTable},
		{ID: "0003_area_index_table", Description: "create area_index table", Up: createAreaIndexTable},
		{ID: "0004_contact_emails_fk", Description: "add contact_emails foreign key", Up: ensureContactEmailsFK},
		{ID: "0005_area_index_fk", Description: "add area_index foreign key", Up: ensureAreaIndexFK},
		{ID: "0006_areas_type_column", Description: "add areas.type column", Up: ensureAreasTypeColumn},
		{ID: "0007_areas_type_index", Description: "add areas.type index", Up: ensureAreasTypeIndex},
		{ID: "0008_areas_is_custom_index", Description: "add areas.is_custom index", Up: ensureAreasIsCustomIndex},
	})
}

func createAreasTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS areas(
			id INT NOT NULL AUTO_INCREMENT,
			name VARCHAR(255) NOT NULL,
			description VARCHAR(255),
			is_custom BOOL NOT NULL DEFAULT false,
			contact_name VARCHAR(255),
			type ENUM('poi', 'admin') NOT NULL DEFAULT 'poi',
			area_json JSON,
			created_at TIMESTAMP,
			updated_at TIMESTAMP,
			PRIMARY KEY (id),
			INDEX type_index (type),
			INDEX is_custom_index (is_custom)
		)`)
	if err != nil {
		return fmt.Errorf("failed to create areas table: %w", err)
	}
	return nil
}

func createContactEmailsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS contact_emails(
			area_id INT NOT NULL,
			email CHAR(64) NOT NULL,
			consent_report BOOL NOT NULL DEFAULT true,
			INDEX area_id_index (area_id),
			INDEX email_index (email)
		)`)
	if err != nil {
		return fmt.Errorf("failed to create contact_emails table: %w", err)
	}
	return nil
}

func createAreaIndexTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS area_index(
			area_id INT NOT NULL,
			geom GEOMETRY NOT NULL SRID 4326,
			SPATIAL INDEX(geom)
		)`)
	if err != nil {
		return fmt.Errorf("failed to create area_index table: %w", err)
	}
	return nil
}

func ensureContactEmailsFK(ctx context.Context, db *sql.DB) error {
	return ensureConstraint(ctx, db, "fk_contact_emails_area_id", `
		ALTER TABLE contact_emails
		ADD CONSTRAINT fk_contact_emails_area_id
		FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
	`)
}

func ensureAreaIndexFK(ctx context.Context, db *sql.DB) error {
	return ensureConstraint(ctx, db, "fk_area_index_area_id", `
		ALTER TABLE area_index
		ADD CONSTRAINT fk_area_index_area_id
		FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
	`)
}

func ensureAreasTypeColumn(ctx context.Context, db *sql.DB) error {
	return ensureColumn(ctx, db, "areas", "type", `
		ALTER TABLE areas
		ADD COLUMN type ENUM('poi', 'admin') NOT NULL DEFAULT 'poi'
	`)
}

func ensureAreasTypeIndex(ctx context.Context, db *sql.DB) error {
	return ensureIndex(ctx, db, "areas", "type_index", `
		ALTER TABLE areas
		ADD INDEX type_index (type)
	`)
}

func ensureAreasIsCustomIndex(ctx context.Context, db *sql.DB) error {
	return ensureIndex(ctx, db, "areas", "is_custom_index", `
		ALTER TABLE areas
		ADD INDEX is_custom_index (is_custom)
	`)
}

func ensureConstraint(ctx context.Context, db *sql.DB, name, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.TABLE_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA = DATABASE() AND CONSTRAINT_NAME = ?
	`, name).Scan(&count); err != nil {
		return fmt.Errorf("failed to check constraint %s: %w", name, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add constraint %s: %w", name, err)
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, tableName, columnName, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, tableName, columnName).Scan(&count); err != nil {
		return fmt.Errorf("failed to check column %s.%s: %w", tableName, columnName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add column %s.%s: %w", tableName, columnName, err)
	}
	return nil
}

func ensureIndex(ctx context.Context, db *sql.DB, tableName, indexName, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?
	`, tableName, indexName).Scan(&count); err != nil {
		return fmt.Errorf("failed to check index %s on %s: %w", indexName, tableName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add index %s on %s: %w", indexName, tableName, err)
	}
	return nil
}
