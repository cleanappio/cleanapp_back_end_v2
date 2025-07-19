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
		area_json JSON,
		created_at TIMESTAMP,
		updated_at TIMESTAMP,
		PRIMARY KEY (id)
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
