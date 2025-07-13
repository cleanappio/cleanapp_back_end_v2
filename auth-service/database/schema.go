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
    INDEX idx_user_method (user_id, method_type),
    INDEX idx_oauth (method_type, oauth_id)
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(256) NOT NULL,
    token_hash VARCHAR(256) NOT NULL,
    token_type ENUM('access', 'refresh') DEFAULT 'access',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
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
		Name:    "add_foreign_key_constraints",
		Up: `
			-- Migration 1: Add foreign key constraints
			-- Add foreign key to login_methods
			SET @dbname = DATABASE();
			SET @tablename = 'login_methods';
			SET @constraintname = 'login_methods_ibfk_1';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND CONSTRAINT_NAME = @constraintname) = 0,
				'ALTER TABLE login_methods ADD CONSTRAINT login_methods_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE;',
				'SELECT 1;'
			));
			PREPARE addFKIfNotExists FROM @preparedStatement;
			EXECUTE addFKIfNotExists;
			DEALLOCATE PREPARE addFKIfNotExists;

			-- Add unique constraint to login_methods
			SET @constraintname = 'unique_user_method';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND CONSTRAINT_NAME = @constraintname) = 0,
				'ALTER TABLE login_methods ADD CONSTRAINT unique_user_method UNIQUE (user_id, method_type);',
				'SELECT 1;'
			));
			PREPARE addUniqueIfNotExists FROM @preparedStatement;
			EXECUTE addUniqueIfNotExists;
			DEALLOCATE PREPARE addUniqueIfNotExists;

			-- Add foreign key to auth_tokens
			SET @tablename = 'auth_tokens';
			SET @constraintname = 'auth_tokens_ibfk_1';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND CONSTRAINT_NAME = @constraintname) = 0,
				'ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_ibfk_1 FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE;',
				'SELECT 1;'
			));
			PREPARE addFKIfNotExists2 FROM @preparedStatement;
			EXECUTE addFKIfNotExists2;
			DEALLOCATE PREPARE addFKIfNotExists2;
		`,
		Down: `
			-- Remove foreign key constraints
			ALTER TABLE auth_tokens DROP FOREIGN KEY IF EXISTS auth_tokens_ibfk_1;
			ALTER TABLE login_methods DROP FOREIGN KEY IF EXISTS login_methods_ibfk_1;
			ALTER TABLE login_methods DROP INDEX IF EXISTS unique_user_method;
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
