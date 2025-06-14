package database

import (
	"database/sql"
	"fmt"
	"log"
)

// Schema contains the database schema
const Schema = `
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;

CREATE TABLE IF NOT EXISTS customers (
    id VARCHAR(256) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    email_encrypted TEXT NOT NULL,
    stripe_customer_id VARCHAR(256),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_stripe_customer (stripe_customer_id)
);

CREATE TABLE IF NOT EXISTS login_methods (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id VARCHAR(256) NOT NULL,
    method_type ENUM('email', 'google', 'apple', 'facebook') NOT NULL,
    password_hash VARCHAR(256),
    oauth_id VARCHAR(256),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    UNIQUE KEY unique_customer_method (customer_id, method_type),
    INDEX idx_oauth (method_type, oauth_id)
);

CREATE TABLE IF NOT EXISTS customer_areas (
    customer_id VARCHAR(256) NOT NULL,
    area_id INT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (customer_id, area_id),
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id VARCHAR(256) NOT NULL,
    plan_type ENUM('base', 'advanced', 'exclusive') NOT NULL,
    billing_cycle ENUM('monthly', 'annual') NOT NULL,
    status ENUM('active', 'suspended', 'cancelled') DEFAULT 'active',
    stripe_subscription_id VARCHAR(256),
    stripe_price_id VARCHAR(256),
    start_date DATE NOT NULL,
    next_billing_date DATE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    INDEX idx_stripe_subscription (stripe_subscription_id)
);

CREATE TABLE IF NOT EXISTS payment_methods (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id VARCHAR(256) NOT NULL,
    stripe_payment_method_id VARCHAR(256) NOT NULL,
    stripe_customer_id VARCHAR(256) NOT NULL,
    last_four VARCHAR(4) NOT NULL,
    brand VARCHAR(50) NOT NULL,
    exp_month INT NOT NULL,
    exp_year INT NOT NULL,
    cardholder_name VARCHAR(256),
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    UNIQUE KEY unique_stripe_payment_method (stripe_payment_method_id),
    INDEX idx_stripe_customer_payment (stripe_customer_id)
);

CREATE TABLE IF NOT EXISTS billing_history (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id VARCHAR(256) NOT NULL,
    subscription_id INT NOT NULL,
    stripe_payment_intent_id VARCHAR(256),
    stripe_invoice_id VARCHAR(256),
    amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    status ENUM('pending', 'completed', 'failed', 'refunded') NOT NULL,
    payment_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id),
    INDEX idx_stripe_payment_intent (stripe_payment_intent_id),
    INDEX idx_stripe_invoice (stripe_invoice_id)
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id VARCHAR(256) NOT NULL,
    token_hash VARCHAR(256) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    INDEX idx_token_hash (token_hash)
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
		Name:    "remove_method_id_from_login_methods",
		Up: `
			-- Migration 1: Remove redundant method_id field
			-- The method_id field was storing duplicate data:
			-- - For email auth: it stored the email (already in customers table)
			-- - For OAuth: it should be oauth_id instead
			
			-- Check if method_id column exists before trying to drop it
			SET @dbname = DATABASE();
			SET @tablename = 'login_methods';
			SET @columnname = 'method_id';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND COLUMN_NAME = @columnname) > 0,
				CONCAT('ALTER TABLE login_methods DROP COLUMN method_id;'),
				'SELECT 1;'
			));
			PREPARE alterIfExists FROM @preparedStatement;
			EXECUTE alterIfExists;
			DEALLOCATE PREPARE alterIfExists;

			-- Add oauth_id column if it doesn't exist
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND COLUMN_NAME = 'oauth_id') = 0,
				'ALTER TABLE login_methods ADD COLUMN oauth_id VARCHAR(256);',
				'SELECT 1;'
			));
			PREPARE alterIfNotExists FROM @preparedStatement;
			EXECUTE alterIfNotExists;
			DEALLOCATE PREPARE alterIfNotExists;

			-- Drop old unique constraint if it exists
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND INDEX_NAME = 'unique_method') > 0,
				'ALTER TABLE login_methods DROP INDEX unique_method;',
				'SELECT 1;'
			));
			PREPARE dropIndexIfExists FROM @preparedStatement;
			EXECUTE dropIndexIfExists;
			DEALLOCATE PREPARE dropIndexIfExists;

			-- Add new unique constraint if it doesn't exist
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND INDEX_NAME = 'unique_customer_method') = 0,
				'ALTER TABLE login_methods ADD UNIQUE KEY unique_customer_method (customer_id, method_type);',
				'SELECT 1;'
			));
			PREPARE addIndexIfNotExists FROM @preparedStatement;
			EXECUTE addIndexIfNotExists;
			DEALLOCATE PREPARE addIndexIfNotExists;

			-- Add oauth index if it doesn't exist
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND INDEX_NAME = 'idx_oauth') = 0,
				'ALTER TABLE login_methods ADD INDEX idx_oauth (method_type, oauth_id);',
				'SELECT 1;'
			));
			PREPARE addOAuthIndexIfNotExists FROM @preparedStatement;
			EXECUTE addOAuthIndexIfNotExists;
			DEALLOCATE PREPARE addOAuthIndexIfNotExists;
		`,
		Down: `
			-- This migration is not reversible as we're removing redundant data
			-- The method_id column contained duplicate information
			SELECT 1;
		`,
	},
	{
		Version: 2,
		Name:    "migrate_to_stripe_payment_methods",
		Up: `
			-- Migration 2: Convert payment methods to use Stripe
			-- This migration transforms the payment_methods table to store Stripe data
			-- instead of encrypted credit card information
			
			-- First, backup existing payment methods if any exist
			CREATE TABLE IF NOT EXISTS payment_methods_backup AS SELECT * FROM payment_methods;
			
			-- Drop the existing payment_methods table
			DROP TABLE IF EXISTS payment_methods;
			
			-- Create new payment_methods table with Stripe fields
			CREATE TABLE payment_methods (
				id INT AUTO_INCREMENT PRIMARY KEY,
				customer_id VARCHAR(256) NOT NULL,
				stripe_payment_method_id VARCHAR(256) NOT NULL,
				stripe_customer_id VARCHAR(256) NOT NULL,
				last_four VARCHAR(4) NOT NULL,
				brand VARCHAR(50) NOT NULL,
				exp_month INT NOT NULL,
				exp_year INT NOT NULL,
				cardholder_name VARCHAR(256),
				is_default BOOLEAN DEFAULT FALSE,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
				UNIQUE KEY unique_stripe_payment_method (stripe_payment_method_id),
				INDEX idx_stripe_customer_payment (stripe_customer_id)
			);
			
			-- Add Stripe customer ID to customers table if it doesn't exist
			SET @dbname = DATABASE();
			SET @tablename = 'customers';
			SET @columnname = 'stripe_customer_id';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = @tablename
				AND COLUMN_NAME = @columnname) = 0,
				'ALTER TABLE customers ADD COLUMN stripe_customer_id VARCHAR(256), ADD INDEX idx_stripe_customer (stripe_customer_id);',
				'SELECT 1;'
			));
			PREPARE alterIfNotExists FROM @preparedStatement;
			EXECUTE alterIfNotExists;
			DEALLOCATE PREPARE alterIfNotExists;
			
			-- Add Stripe fields to subscriptions table
			SET @columnname = 'stripe_subscription_id';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = 'subscriptions'
				AND COLUMN_NAME = @columnname) = 0,
				'ALTER TABLE subscriptions ADD COLUMN stripe_subscription_id VARCHAR(256), ADD COLUMN stripe_price_id VARCHAR(256), ADD INDEX idx_stripe_subscription (stripe_subscription_id);',
				'SELECT 1;'
			));
			PREPARE alterIfNotExists FROM @preparedStatement;
			EXECUTE alterIfNotExists;
			DEALLOCATE PREPARE alterIfNotExists;
			
			-- Add Stripe fields to billing_history table
			SET @columnname = 'stripe_payment_intent_id';
			SET @preparedStatement = (SELECT IF(
				(SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @dbname
				AND TABLE_NAME = 'billing_history'
				AND COLUMN_NAME = @columnname) = 0,
				'ALTER TABLE billing_history ADD COLUMN stripe_payment_intent_id VARCHAR(256), ADD COLUMN stripe_invoice_id VARCHAR(256), ADD INDEX idx_stripe_payment_intent (stripe_payment_intent_id), ADD INDEX idx_stripe_invoice (stripe_invoice_id);',
				'SELECT 1;'
			));
			PREPARE alterIfNotExists FROM @preparedStatement;
			EXECUTE alterIfNotExists;
			DEALLOCATE PREPARE alterIfNotExists;
		`,
		Down: `
			-- Restore from backup if needed
			-- This is a destructive migration, so we keep the backup table
			-- Manual intervention would be required to restore encrypted card data
			SELECT 1;
		`,
	},
}

// InitializeSchema creates the database schema and runs migrations
func InitializeSchema(db *sql.DB) error {
	// Create initial schema
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Run migrations
	if err := RunMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// RunMigrations executes all pending migrations
func RunMigrations(db *sql.DB) error {
	// Ensure migrations table exists
	if _, err := db.Exec("USE cleanapp"); err != nil {
		return err
	}

	// Get current schema version
	var currentVersion int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		// Table might not exist in fresh installations, that's ok
		currentVersion = 0
	}

	// Run pending migrations
	for _, migration := range Migrations {
		if migration.Version > currentVersion {
			log.Printf("Running migration %d: %s", migration.Version, migration.Name)
			
			// Start transaction
			tx, err := db.Begin()
			if err != nil {
				return fmt.Errorf("failed to start transaction for migration %d: %w", migration.Version, err)
			}

			// Execute migration
			if _, err := tx.Exec(migration.Up); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to execute migration %d: %w", migration.Version, err)
			}

			// Record migration
			if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migration.Version); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
			}

			// Commit transaction
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
			}

			log.Printf("Migration %d completed successfully", migration.Version)
		}
	}

	return nil
}
