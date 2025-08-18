package database

import (
	"database/sql"
	"fmt"
	"log"
)

// Schema contains the database schema for report authorization
const Schema = `
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;

-- Note: This service relies on existing tables in the cleanapp database:
-- - reports
-- - report_analysis  
-- - customer_areas
-- - areas
-- - area_index
-- - customer_brands
`

// InitializeSchema creates the database schema and runs migrations
func InitializeSchema(db *sql.DB) error {
	// Create tables
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Println("Database schema initialized successfully")
	return nil
}
