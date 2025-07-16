package database

import (
	"database/sql"
	"fmt"
	"log"
)

// RunMigrations runs all database migrations
func RunMigrations(db *sql.DB) error {
	log.Println("Running database migrations...")

	// Migration 1: Add language field to report_analysis table
	if err := runMigration001(db); err != nil {
		return fmt.Errorf("migration 001 failed: %w", err)
	}

	// Migration 2: Add brand_name field to report_analysis table
	if err := runMigration002(db); err != nil {
		return fmt.Errorf("migration 002 failed: %w", err)
	}

	log.Println("All migrations completed successfully")
	return nil
}

// runMigration001 adds language field to report_analysis table
func runMigration001(db *sql.DB) error {
	log.Println("Running migration 001: Adding language field to report_analysis table")

	// Step 1: Add language column (will fail if already exists, but that's ok)
	_, err := db.Exec(`
		ALTER TABLE report_analysis 
		ADD COLUMN language VARCHAR(2) NOT NULL DEFAULT 'en'
	`)
	if err != nil {
		log.Printf("Note: language column may already exist: %v", err)
	}

	// Step 2: Update existing records with empty language to 'en'
	_, err = db.Exec(`
		UPDATE report_analysis 
		SET language = 'en' 
		WHERE language IS NULL OR language = ''
	`)
	if err != nil {
		return fmt.Errorf("failed to update existing records: %w", err)
	}

	// Step 3: Add index on language column
	_, err = db.Exec(`
		CREATE INDEX idx_report_analysis_language 
		ON report_analysis(language)
	`)
	if err != nil {
		log.Printf("Note: language index may already exist: %v", err)
	}

	log.Println("Migration 001 completed")
	return nil
}

// runMigration002 adds brand_name field to report_analysis table
func runMigration002(db *sql.DB) error {
	log.Println("Running migration 002: Adding brand_name field to report_analysis table")

	// Step 1: Add brand_name column (will fail if already exists, but that's ok)
	_, err := db.Exec(`
		ALTER TABLE report_analysis 
		ADD COLUMN brand_name VARCHAR(255) DEFAULT ''
	`)
	if err != nil {
		log.Printf("Note: brand_name column may already exist: %v", err)
	}

	// Step 2: Update existing records with empty brand_name to empty string
	_, err = db.Exec(`
		UPDATE report_analysis 
		SET brand_name = '' 
		WHERE brand_name IS NULL
	`)
	if err != nil {
		return fmt.Errorf("failed to update existing records: %w", err)
	}

	// Step 3: Add index on brand_name column
	_, err = db.Exec(`
		CREATE INDEX idx_report_analysis_brand_name 
		ON report_analysis(brand_name)
	`)
	if err != nil {
		log.Printf("Note: brand_name index may already exist: %v", err)
	}

	log.Println("Migration 002 completed")
	return nil
}
