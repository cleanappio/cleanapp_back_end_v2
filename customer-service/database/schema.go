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
    is_public BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (customer_id, area_id),
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE,
    INDEX idx_customer_areas_is_public (is_public)
);

CREATE TABLE IF NOT EXISTS customer_brands (
    customer_id VARCHAR(256) NOT NULL,
    brand_name VARCHAR(255) NOT NULL,
    is_public BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (customer_id, brand_name),
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    INDEX idx_customer_brands_is_public (is_public)
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
`

// InitializeSchema creates the database schema and runs migrations
func InitializeSchema(db *sql.DB) error {
	// Create tables
	if _, err := db.Exec(Schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Run migrations for existing tables
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Println("Database schema initialized successfully")
	return nil
}

// runMigrations handles schema migrations for existing tables
func runMigrations(db *sql.DB) error {
	log.Println("Running database migrations...")

	// Migration: Add area_id foreign key constraint to customer_areas table if it doesn't exist
	if err := addAreaIdForeignKeyToCustomerAreas(db); err != nil {
		return fmt.Errorf("failed to add area_id foreign key to customer_areas table: %w", err)
	}

	// Migration: Add customer_id foreign key constraint to customer_areas table if it doesn't exist
	if err := addCustomerIdForeignKeyToCustomerAreas(db); err != nil {
		return fmt.Errorf("failed to add customer_id foreign key to customer_areas table: %w", err)
	}

	// Migration: Add is_public field to customer_areas table if it doesn't exist
	if err := addIsPublicFieldToCustomerAreas(db); err != nil {
		return fmt.Errorf("failed to add is_public field to customer_areas table: %w", err)
	}

	// Migration: Add is_public field to customer_brands table if it doesn't exist
	if err := addIsPublicFieldToCustomerBrands(db); err != nil {
		return fmt.Errorf("failed to add is_public field to customer_brands table: %w", err)
	}

	log.Println("Database migrations completed successfully")
	return nil
}

// addAreaIdForeignKeyToCustomerAreas adds the area_id foreign key constraint to customer_areas table if it doesn't exist
func addAreaIdForeignKeyToCustomerAreas(db *sql.DB) error {
	// Check if area_id foreign key constraint already exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'customer_areas'
		AND REFERENCED_TABLE_NAME = 'areas';`).Scan(&count)

	if err != nil {
		log.Printf("Could not check for existing area_id foreign key constraint: %v", err)
		return err
	}

	if count == 0 {
		// Add foreign key constraint for area_id
		_, err := db.Exec(`
			ALTER TABLE customer_areas 
			ADD CONSTRAINT fk_customer_areas_area_id 
			FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
		`)
		if err != nil {
			log.Printf("Could not add area_id foreign key constraint to customer_areas table: %v", err)
			return err
		}
		log.Println("Added area_id foreign key constraint to customer_areas table")
	} else {
		log.Println("Area_id foreign key constraint already exists in customer_areas table")
	}

	return nil
}

// addCustomerIdForeignKeyToCustomerAreas adds the area_id foreign key constraint to customer_areas table if it doesn't exist
func addCustomerIdForeignKeyToCustomerAreas(db *sql.DB) error {
	// Check if area_id foreign key constraint already exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'customer_areas'
		AND REFERENCED_TABLE_NAME = 'customers';`).Scan(&count)

	if err != nil {
		log.Printf("Could not check for existing customer_id foreign key constraint: %v", err)
		return err
	}

	if count == 0 {
		// Add foreign key constraint for customer_id
		_, err := db.Exec(`
			ALTER TABLE customer_areas 
			ADD CONSTRAINT fk_customer_areas_customer_id 
			FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
		`)
		if err != nil {
			log.Printf("Could not add customer_id foreign key constraint to customer_areas table: %v", err)
			return err
		}
		log.Println("Added customer_id foreign key constraint to customer_areas table")
	} else {
		log.Println("Customer_id foreign key constraint already exists in customer_areas table")
	}

	return nil
}

// addIsPublicFieldToCustomerAreas adds the is_public field to customer_areas table if it doesn't exist
func addIsPublicFieldToCustomerAreas(db *sql.DB) error {
	// Check if is_public column already exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'customer_areas'
		AND COLUMN_NAME = 'is_public';`).Scan(&count)

	if err != nil {
		log.Printf("Could not check for existing is_public column: %v", err)
		return err
	}

	if count == 0 {
		// Add is_public column
		_, err := db.Exec(`
			ALTER TABLE customer_areas 
			ADD COLUMN is_public BOOLEAN DEFAULT FALSE
		`)
		if err != nil {
			log.Printf("Could not add is_public column to customer_areas table: %v", err)
			return err
		}

		// Add index for is_public column
		_, err = db.Exec(`
			ALTER TABLE customer_areas 
			ADD INDEX idx_customer_areas_is_public (is_public)
		`)
		if err != nil {
			log.Printf("Could not add is_public index to customer_areas table: %v", err)
			return err
		}

		log.Println("Added is_public field and index to customer_areas table")
	} else {
		log.Println("is_public field already exists in customer_areas table")
	}

	return nil
}

// addIsPublicFieldToCustomerBrands adds the is_public field to customer_brands table if it doesn't exist
func addIsPublicFieldToCustomerBrands(db *sql.DB) error {
	// Check if is_public column already exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = 'customer_brands'
		AND COLUMN_NAME = 'is_public';`).Scan(&count)

	if err != nil {
		log.Printf("Could not check for existing is_public column: %v", err)
		return err
	}

	if count == 0 {
		// Add is_public column
		_, err := db.Exec(`
			ALTER TABLE customer_brands 
			ADD COLUMN is_public BOOLEAN DEFAULT FALSE
		`)
		if err != nil {
			log.Printf("Could not add is_public column to customer_brands table: %v", err)
			return err
		}

		// Add index for is_public column
		_, err = db.Exec(`
			ALTER TABLE customer_brands 
			ADD INDEX idx_customer_brands_is_public (is_public)
		`)
		if err != nil {
			log.Printf("Could not add is_public index to customer_brands table: %v", err)
			return err
		}

		log.Println("Added is_public field and index to customer_brands table")
	} else {
		log.Println("is_public field already exists in customer_brands table")
	}

	return nil
}
