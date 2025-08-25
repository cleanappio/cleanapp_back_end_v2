package database

import (
	"database/sql"
	"fmt"

	"github.com/apex/log"
)

// InitSchema creates the necessary database tables if they don't exist
func InitSchema(db *sql.DB) error {
	log.Info("Initializing GDPR process service database schema...")

	// Create users_gdpr table
	usersGdprTableSQL := `
	CREATE TABLE IF NOT EXISTS users_gdpr(
		id VARCHAR(255) NOT NULL,
		processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		UNIQUE INDEX id_unique (id)
	)`

	if _, err := db.Exec(usersGdprTableSQL); err != nil {
		return fmt.Errorf("failed to create users_gdpr table: %w", err)
	}
	log.Info("users_gdpr table created/verified")

	// Create reports_gdpr table
	reportsGdprTableSQL := `
	CREATE TABLE IF NOT EXISTS reports_gdpr(
		seq INT NOT NULL,
		processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (seq),
		UNIQUE INDEX seq_unique (seq)
	)`

	if _, err := db.Exec(reportsGdprTableSQL); err != nil {
		return fmt.Errorf("failed to create reports_gdpr table: %w", err)
	}
	log.Info("reports_gdpr table created/verified")

	log.Info("GDPR process service database schema initialization completed")
	return nil
}
