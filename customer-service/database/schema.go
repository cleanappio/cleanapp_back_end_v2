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

CREATE TABLE IF NOT EXISTS customer_brands (
    customer_id VARCHAR(256) NOT NULL,
    brand_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (customer_id, brand_name),
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
