package database

import (
	"database/sql"
	"fmt"

	"github.com/apex/log"
)

// InitSchema creates the necessary database tables if they don't exist
func InitSchema(db *sql.DB) error {
	log.Info("Initializing areas-service database schema...")

	// Create areas table
	areasTableSQL := `
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
	)`

	if _, err := db.Exec(areasTableSQL); err != nil {
		return fmt.Errorf("failed to create areas table: %w", err)
	}
	log.Info("Areas table created/verified")

	// Create contact_emails table
	contactEmailsTableSQL := `
	CREATE TABLE IF NOT EXISTS contact_emails(
		area_id INT NOT NULL,
		email CHAR(64) NOT NULL,
		consent_report BOOL NOT NULL DEFAULT true,
		INDEX area_id_index (area_id),
		INDEX email_index (email)
	)`

	if _, err := db.Exec(contactEmailsTableSQL); err != nil {
		return fmt.Errorf("failed to create contact_emails table: %w", err)
	}
	log.Info("Contact_emails table created/verified")

	// Create area_index table
	areaIndexTableSQL := `
	CREATE TABLE IF NOT EXISTS area_index(
		area_id INT NOT NULL,
		geom GEOMETRY NOT NULL SRID 4326,
		SPATIAL INDEX(geom)
	)`

	if _, err := db.Exec(areaIndexTableSQL); err != nil {
		return fmt.Errorf("failed to create area_index table: %w", err)
	}
	log.Info("Area_index table created/verified")

	// Add foreign key constraints if they don't exist
	// Note: We'll try to add them but ignore errors if they already exist
	addFKConstraints(db)

	// Run migrations for existing tables
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("Areas-service database schema initialization completed")
	return nil
}

// addFKConstraints adds foreign key constraints for referential integrity
func addFKConstraints(db *sql.DB) {
	// Check if foreign key constraints already exist
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.TABLE_CONSTRAINTS 
		WHERE CONSTRAINT_SCHEMA = DATABASE() 
		AND CONSTRAINT_NAME = 'fk_contact_emails_area_id'
	`).Scan(&count)

	if err != nil {
		log.Warnf("Could not check for existing foreign key constraints: %v", err)
		return
	}

	if count == 0 {
		// Add foreign key constraint for contact_emails
		_, err := db.Exec(`
			ALTER TABLE contact_emails 
			ADD CONSTRAINT fk_contact_emails_area_id 
			FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
		`)
		if err != nil {
			log.Warnf("Could not add foreign key constraint for contact_emails: %v", err)
		} else {
			log.Info("Added foreign key constraint for contact_emails")
		}
	}

	// Check for area_index foreign key
	err = db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.TABLE_CONSTRAINTS 
		WHERE CONSTRAINT_SCHEMA = DATABASE() 
		AND CONSTRAINT_NAME = 'fk_area_index_area_id'
	`).Scan(&count)

	if err != nil {
		log.Warnf("Could not check for existing area_index foreign key constraint: %v", err)
		return
	}

	if count == 0 {
		// Add foreign key constraint for area_index
		_, err := db.Exec(`
			ALTER TABLE area_index 
			ADD CONSTRAINT fk_area_index_area_id 
			FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
		`)
		if err != nil {
			log.Warnf("Could not add foreign key constraint for area_index: %v", err)
		} else {
			log.Info("Added foreign key constraint for area_index")
		}
	}
}

// runMigrations handles schema migrations for existing tables
func runMigrations(db *sql.DB) error {
	log.Info("Running database migrations...")

	// Migration 1: Add type field to areas table if it doesn't exist
	if err := addTypeFieldToAreas(db); err != nil {
		return fmt.Errorf("failed to add type field to areas table: %w", err)
	}

	// Migration 2: Add type_index to areas table if it doesn't exist
	if err := addTypeIndexToAreas(db); err != nil {
		return fmt.Errorf("failed to add type_index to areas table: %w", err)
	}

	// Migration 3: Add is_custom_index to areas table if it doesn't exist
	if err := addIsCustomIndexToAreas(db); err != nil {
		return fmt.Errorf("failed to add is_custom_index to areas table: %w", err)
	}

	log.Info("Database migrations completed successfully")
	return nil
}

// addTypeFieldToAreas adds the type field to the areas table if it doesn't exist
func addTypeFieldToAreas(db *sql.DB) error {
	// Check if type column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'areas' 
		AND COLUMN_NAME = 'type'
	`).Scan(&count)

	if err != nil {
		log.Warnf("Could not check if type column exists: %v", err)
		return err
	}

	if count == 0 {
		// Add type column
		_, err := db.Exec(`
			ALTER TABLE areas 
			ADD COLUMN type ENUM('poi', 'admin') NOT NULL DEFAULT 'poi'
		`)
		if err != nil {
			log.Warnf("Could not add type column to areas table: %v", err)
			return err
		}
		log.Info("Added type column to areas table")
	} else {
		log.Info("Type column already exists in areas table")
	}

	return nil
}

// addTypeIndexToAreas adds the type_index to the areas table if it doesn't exist
func addTypeIndexToAreas(db *sql.DB) error {
	// Check if type_index exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.STATISTICS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'areas' 
		AND INDEX_NAME = 'type_index'
	`).Scan(&count)

	if err != nil {
		log.Warnf("Could not check if type_index exists: %v", err)
		return err
	}

	if count == 0 {
		// Add type_index
		_, err := db.Exec(`
			ALTER TABLE areas 
			ADD INDEX type_index (type)
		`)
		if err != nil {
			log.Warnf("Could not add type_index to areas table: %v", err)
			return err
		}
		log.Info("Added type_index to areas table")
	} else {
		log.Info("Type_index already exists in areas table")
	}

	return nil
}

// addIsCustomIndexToAreas adds the is_custom_index to the areas table if it doesn't exist
func addIsCustomIndexToAreas(db *sql.DB) error {
	// Check if is_custom_index exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.STATISTICS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'areas' 
		AND INDEX_NAME = 'is_custom_index'
	`).Scan(&count)

	if err != nil {
		log.Warnf("Could not check if is_custom_index exists: %v", err)
		return err
	}

	if count == 0 {
		// Add is_custom_index
		_, err := db.Exec(`
			ALTER TABLE areas 
			ADD INDEX is_custom_index (is_custom)
		`)
		if err != nil {
			log.Warnf("Could not add is_custom_index to areas table: %v", err)
			return err
		}
		log.Info("Added is_custom_index to areas table")
	} else {
		log.Info("Is_custom_index already exists in areas table")
	}

	return nil
}
