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
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (seq, owner),
    INDEX idx_seq (seq)
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

	log.Println("Database schema initialized successfully")
	return nil
}
