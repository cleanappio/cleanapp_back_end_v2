package database

import (
	"database/sql"
	"fmt"
	"log"
)

// Schema contains the database schema for authentication
const Schema = `
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;

CREATE TABLE IF NOT EXISTS client_auth (
    id VARCHAR(256) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    email_encrypted TEXT NOT NULL,
    sync_version INT DEFAULT 1,
    last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_email_encrypted (email_encrypted(255)),
    INDEX idx_sync_version (sync_version)
);

CREATE TABLE IF NOT EXISTS login_methods (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(256) NOT NULL,
    method_type ENUM('email', 'google', 'apple', 'facebook') NOT NULL,
    password_hash VARCHAR(256),
    oauth_id VARCHAR(256),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE,
    UNIQUE KEY unique_user_method (user_id, method_type),
    INDEX idx_oauth (method_type, oauth_id)
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(256) NOT NULL,
    token_hash VARCHAR(256) NOT NULL,
    token_type ENUM('access', 'refresh') DEFAULT 'access',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE,
    INDEX idx_user_token_type (user_id, token_type)
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INT PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

// Migration represents a database migration
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

// Migrations list all database migrations
var Migrations = []Migration{
	{
		Version: 1,
		Name:    "add_token_type_to_auth_tokens",
		Up: `
			-- Migration 1: Add token_type to auth_tokens table
			SET @dbname = DATABASE();
			SET @tablename = 'auth_tokens';
			SET @columnname = 'token_type';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND COLUMN_NAME = @columnname) = 0,
				'ALTER TABLE auth_tokens ADD COLUMN token_type ENUM("access", "refresh") DEFAULT "access";',
				'SELECT 1;'
			));
			PREPARE alterIfNotExists FROM @preparedStatement;
			EXECUTE alterIfNotExists;
			DEALLOCATE PREPARE alterIfNotExists;

			-- Add index for user_id and token_type
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND INDEX_NAME = 'idx_user_token_type') = 0,
				'ALTER TABLE auth_tokens ADD INDEX idx_user_token_type (user_id, token_type);',
				'SELECT 1;'
			));
			PREPARE addIndexIfNotExists FROM @preparedStatement;
			EXECUTE addIndexIfNotExists;
			DEALLOCATE PREPARE addIndexIfNotExists;
		`,
		Down: `
			-- Remove token_type column and index
			ALTER TABLE auth_tokens DROP INDEX IF EXISTS idx_user_token_type;
			ALTER TABLE auth_tokens DROP COLUMN IF EXISTS token_type;
		`,
	},
}

// InitializeSchema creates the database schema and runs migrations
func InitializeSchema(db *sql.DB) error {
	// Create tables
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Run migrations
	if err := RunMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Println("Database schema initialized successfully")
	return nil
}

// RunMigrations applies all pending database migrations
func RunMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get applied migrations
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[version] = true
	}

	// Apply pending migrations
	for _, migration := range Migrations {
		if !applied[migration.Version] {
			log.Printf("Applying migration %d: %s", migration.Version, migration.Name)

			// Apply migration
			if _, err := db.Exec(migration.Up); err != nil {
				return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
			}

			// Record migration
			if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migration.Version); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
			}

			log.Printf("Migration %d applied successfully", migration.Version)
		}
	}

	return nil
}
