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
    stripe_customer_id VARCHAR(256),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_stripe_customer (stripe_customer_id)
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
    status ENUM('active', 'incomplete', 'suspended', 'cancelled') DEFAULT 'active',
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
		Name:    "migrate_to_stripe_payment_methods",
		Up: `
			-- Migration 1: Convert payment methods to use Stripe
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
	{
		Version: 2,
		Name:    "add_stripe_integration_support",
		Up: `
        -- Migration 2: Add full Stripe integration support
        
        -- Add trial and cancellation fields to subscriptions
        SET @dbname = DATABASE();
        SET @tablename = 'subscriptions';
        
        -- Add trial_end column
        SET @columnname = 'trial_end';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE subscriptions ADD COLUMN trial_end TIMESTAMP NULL;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add cancel_at_period_end column
        SET @columnname = 'cancel_at_period_end';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE subscriptions ADD COLUMN cancel_at_period_end BOOLEAN DEFAULT FALSE;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add canceled_at column
        SET @columnname = 'canceled_at';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE subscriptions ADD COLUMN canceled_at TIMESTAMP NULL;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add ended_at column
        SET @columnname = 'ended_at';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE subscriptions ADD COLUMN ended_at TIMESTAMP NULL;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add additional fields to billing_history
        SET @tablename = 'billing_history';
        
        -- Add stripe_charge_id column
        SET @columnname = 'stripe_charge_id';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE billing_history ADD COLUMN stripe_charge_id VARCHAR(256);',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add failure_reason column
        SET @columnname = 'failure_reason';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE billing_history ADD COLUMN failure_reason TEXT;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Add refund_amount column
        SET @columnname = 'refund_amount';
        SET @preparedStatement = (SELECT IF(
            (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND COLUMN_NAME = @columnname) = 0,
            'ALTER TABLE billing_history ADD COLUMN refund_amount DECIMAL(10, 2) DEFAULT 0;',
            'SELECT 1;'
        ));
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
        
        -- Create webhook_events table for idempotency
        CREATE TABLE IF NOT EXISTS webhook_events (
            id VARCHAR(256) PRIMARY KEY,
            type VARCHAR(100) NOT NULL,
            processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            raw_data JSON,
            status ENUM('processed', 'failed', 'skipped') DEFAULT 'processed',
            error_message TEXT,
            INDEX idx_type_processed (type, processed_at),
            INDEX idx_status (status)
        );
        
        -- Create stripe_sync_log table for tracking sync operations
        CREATE TABLE IF NOT EXISTS stripe_sync_log (
            id INT AUTO_INCREMENT PRIMARY KEY,
            entity_type VARCHAR(50) NOT NULL,
            entity_id VARCHAR(256) NOT NULL,
            stripe_id VARCHAR(256),
            action VARCHAR(50) NOT NULL,
            status ENUM('success', 'failed') NOT NULL,
            error_message TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_entity (entity_type, entity_id),
            INDEX idx_stripe_id (stripe_id),
            INDEX idx_created (created_at)
        );
    `,
		Down: `
        -- Remove added columns from subscriptions
        ALTER TABLE subscriptions 
            DROP COLUMN IF EXISTS trial_end,
            DROP COLUMN IF EXISTS cancel_at_period_end,
            DROP COLUMN IF EXISTS canceled_at,
            DROP COLUMN IF EXISTS ended_at;
        
        -- Remove added columns from billing_history
        ALTER TABLE billing_history 
            DROP COLUMN IF EXISTS stripe_charge_id,
            DROP COLUMN IF EXISTS failure_reason,
            DROP COLUMN IF EXISTS refund_amount;
        
        -- Drop webhook_events table
        DROP TABLE IF EXISTS webhook_events;
        
        -- Drop stripe_sync_log table
        DROP TABLE IF EXISTS stripe_sync_log;
        
        -- Note: We don't remove Stripe ID columns as they may contain important data
        -- and were potentially added in earlier migrations
    `,
	},
	{
		Version: 3,
		Name:    "change_subscription_status_enum_field",
		Up: `
        SET @tablename = 'subscriptions';

        SET @columnname = 'status';
        SET @preparedStatement = (SELECT 'ALTER TABLE subscriptions CHANGE COLUMN status status ENUM(''active'', ''incomplete'', ''suspended'', ''cancelled'') DEFAULT ''active'';');
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
		`,
		Down: `
				ALTER TABLE subscriptions CHANGE COLUMN status status ENUM('active', 'suspended', 'cancelled') DEFAULT 'active';
		`,
	},
	{
		Version: 4,
		Name:    "add_subscription_stripe_id_unique_index",
		Up: `
				SET @dbname = DATABASE();
				SET @tablename = 'subscriptions';
				SET @indexname = 'idx_stripe_subscription';
				SET @preparedStatement = (SELECT IF(
					(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS 
            WHERE TABLE_SCHEMA = @dbname
            AND TABLE_NAME = @tablename
            AND INDEX_NAME = @indexname) = 1,
					'ALTER TABLE subscriptions DROP INDEX idx_stripe_subscription;',
					'SELECT 1;'
				));
				PREPARE alterIfNotExists FROM @preparedStatement;
				EXECUTE alterIfNotExists;
				DEALLOCATE PREPARE alterIfNotExists;

				SET @preparedStatement = (SELECT IF(
					(SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
						WHERE TABLE_SCHEMA = @dbname
						AND TABLE_NAME = @tablename
						AND INDEX_NAME = @indexname) = 0,
					'ALTER TABLE subscriptions ADD UNIQUE INDEX idx_stripe_subscription(stripe_subscription_id);',
					'SELECT 1;'
				));
				PREPARE alterIfNotExists FROM @preparedStatement;
				EXECUTE alterIfNotExists;
				DEALLOCATE PREPARE alterIfNotExists;
			`,
		Down: `
				ALTER TABLE subscriptions DROP INDEX IF EXISTS idx_stripe_subscription;
		`,
	},
	{
		Version: 5,
		Name:    "fix_subscription_status_enum",
		Up: `
        SET @tablename = 'subscriptions';

        SET @columnname = 'status';
        SET @preparedStatement = (SELECT 'ALTER TABLE subscriptions CHANGE COLUMN status status ENUM(''active'', ''suspended'', ''canceled'') DEFAULT ''active'';');
        PREPARE alterIfNotExists FROM @preparedStatement;
        EXECUTE alterIfNotExists;
        DEALLOCATE PREPARE alterIfNotExists;
		`,
		Down: `
				ALTER TABLE subscriptions CHANGE COLUMN status status ENUM('active', 'incomplete', 'suspended', 'cancelled') DEFAULT 'active';
		`,
	},
	{
		Version: 6,
		Name:    "normalize_customers_table",
		Up: `
        -- Migration 6: Normalize customers table
        -- Remove redundant fields (name, email_encrypted) from customers table
        -- These fields are now managed by the auth-service in client_auth table
        -- Remove sync fields as they are no longer needed

        -- Remove name column from customers table
        ALTER TABLE customers DROP COLUMN IF EXISTS name;

        -- Remove email_encrypted column from customers table  
        ALTER TABLE customers DROP COLUMN IF EXISTS email_encrypted;

        -- Remove sync-related columns
        ALTER TABLE customers DROP COLUMN IF EXISTS sync_version;
        ALTER TABLE customers DROP COLUMN IF EXISTS last_sync_at;

        -- Remove sync-related indexes
        DROP INDEX IF EXISTS idx_sync_version ON customers;

        -- Update customers table structure to be subscription-focused
        -- Now customers table only contains subscription-related data
        -- The relationship with auth data is maintained via the id field
		`,
		Down: `
        -- Add back the removed columns (for rollback purposes)
        ALTER TABLE customers 
            ADD COLUMN name VARCHAR(256) NOT NULL DEFAULT '',
            ADD COLUMN email_encrypted TEXT NOT NULL DEFAULT '',
            ADD COLUMN sync_version INT DEFAULT 1,
            ADD COLUMN last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            ADD INDEX idx_sync_version (sync_version);
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
		return fmt.Errorf("failed to query applied migrations: %w", err)
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

			if _, err := db.Exec(migration.Up); err != nil {
				return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
			}

			if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migration.Version); err != nil {
				return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
			}

			log.Printf("Applied migration %d: %s", migration.Version, migration.Name)
		}
	}

	return nil
}
