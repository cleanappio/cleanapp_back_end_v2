package database

import (
	"database/sql"
	"fmt"
	"log"
)

const Schema = `
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;

-- Table to store report ownership information
CREATE TABLE IF NOT EXISTS reports_owners (
    seq INT NOT NULL,
    owner VARCHAR(256) NOT NULL,
    is_public BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (seq, owner),
    INDEX idx_seq (seq),
    INDEX idx_is_public (is_public)
);

-- Note: This service relies on existing tables in the cleanapp database:
-- - reports
-- - report_analysis
-- - area_index
-- - customer_areas
-- - customer_brands
`

// InitializeSchema creates the necessary database tables
func InitializeSchema(db *sql.DB) error {
	log.Println("Initializing database schema...")

	// Execute the schema
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Check if is_public field exists and add it if it doesn't
	if err := ensureIsPublicFieldExists(db); err != nil {
		return fmt.Errorf("failed to ensure is_public field exists: %w", err)
	}

	log.Println("Database schema initialized successfully")
	return nil
}

// ensureIsPublicFieldExists checks if the is_public field exists and adds it if it doesn't
func ensureIsPublicFieldExists(db *sql.DB) error {
	// Check if the is_public column exists
	var columnExists bool
	query := `
		SELECT COUNT(*) > 0 
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_SCHEMA = 'cleanapp' 
		AND TABLE_NAME = 'reports_owners' 
		AND COLUMN_NAME = 'is_public'
	`

	err := db.QueryRow(query).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check if is_public column exists: %w", err)
	}

	if !columnExists {
		log.Println("Adding is_public field to reports_owners table...")

		// Add the is_public column
		alterQuery := `
			ALTER TABLE reports_owners 
			ADD COLUMN is_public BOOLEAN DEFAULT FALSE,
			ADD INDEX idx_is_public (is_public)
		`

		if _, err := db.Exec(alterQuery); err != nil {
			return fmt.Errorf("failed to add is_public column: %w", err)
		}

		log.Println("Successfully added is_public field and index")
	} else {
		log.Println("is_public field already exists in reports_owners table")
	}

	return nil
}
