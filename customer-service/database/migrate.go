package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/migrator"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return migrator.Run(ctx, db, "customer-service", []migrator.Step{
		{ID: "0001_customers", Description: "create customers table", Up: createCustomersTable},
		{ID: "0002_customer_areas", Description: "create customer_areas table", Up: createCustomerAreasTable},
		{ID: "0003_customer_brands", Description: "create customer_brands table", Up: createCustomerBrandsTable},
		{ID: "0004_subscriptions", Description: "create subscriptions table", Up: createSubscriptionsTable},
		{ID: "0005_payment_methods", Description: "create payment_methods table", Up: createPaymentMethodsTable},
		{ID: "0006_billing_history", Description: "create billing_history table", Up: createBillingHistoryTable},
		{ID: "0007_customer_areas_fks", Description: "ensure customer_areas foreign keys", Up: migrateCustomerAreasForeignKeys},
		{ID: "0008_customer_areas_is_public", Description: "ensure customer_areas visibility fields", Up: addCustomerAreasIsPublic},
		{ID: "0009_customer_brands_is_public", Description: "ensure customer_brands visibility fields", Up: addCustomerBrandsIsPublic},
	})
}

func createCustomersTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS customers (
			id VARCHAR(256) PRIMARY KEY,
			stripe_customer_id VARCHAR(256),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_stripe_customer (stripe_customer_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create customers table: %w", err)
	}
	return nil
}

func createCustomerAreasTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS customer_areas (
			customer_id VARCHAR(256) NOT NULL,
			area_id INT NOT NULL,
			is_public BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (customer_id, area_id),
			FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
			FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE,
			INDEX idx_customer_areas_is_public (is_public)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create customer_areas table: %w", err)
	}
	return nil
}

func createCustomerBrandsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS customer_brands (
			customer_id VARCHAR(256) NOT NULL,
			brand_name VARCHAR(255) NOT NULL,
			is_public BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (customer_id, brand_name),
			FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
			INDEX idx_customer_brands_is_public (is_public)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create customer_brands table: %w", err)
	}
	return nil
}

func createSubscriptionsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
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
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create subscriptions table: %w", err)
	}
	return nil
}

func createPaymentMethodsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
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
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create payment_methods table: %w", err)
	}
	return nil
}

func createBillingHistoryTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
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
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create billing_history table: %w", err)
	}
	return nil
}

func migrateCustomerAreasForeignKeys(ctx context.Context, db *sql.DB) error {
	if err := addAreaIdForeignKeyToCustomerAreas(db); err != nil {
		return err
	}
	if err := addCustomerIdForeignKeyToCustomerAreas(db); err != nil {
		return err
	}
	return nil
}

func addCustomerAreasIsPublic(ctx context.Context, db *sql.DB) error {
	return addIsPublicFieldToCustomerAreas(db)
}

func addCustomerBrandsIsPublic(ctx context.Context, db *sql.DB) error {
	return addIsPublicFieldToCustomerBrands(db)
}

func addAreaIdForeignKeyToCustomerAreas(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_areas'
		AND REFERENCED_TABLE_NAME = 'areas'`).Scan(&count); err != nil {
		return fmt.Errorf("check customer_areas.area_id foreign key: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err := db.Exec(`
		ALTER TABLE customer_areas
		ADD CONSTRAINT fk_customer_areas_area_id
		FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE
	`)
	if err != nil {
		return fmt.Errorf("add customer_areas.area_id foreign key: %w", err)
	}
	return nil
}

func addCustomerIdForeignKeyToCustomerAreas(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_areas'
		AND REFERENCED_TABLE_NAME = 'customers'`).Scan(&count); err != nil {
		return fmt.Errorf("check customer_areas.customer_id foreign key: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err := db.Exec(`
		ALTER TABLE customer_areas
		ADD CONSTRAINT fk_customer_areas_customer_id
		FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
	`)
	if err != nil {
		return fmt.Errorf("add customer_areas.customer_id foreign key: %w", err)
	}
	return nil
}

func addIsPublicFieldToCustomerAreas(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_areas'
		AND COLUMN_NAME = 'is_public'`).Scan(&count); err != nil {
		return fmt.Errorf("check customer_areas.is_public column: %w", err)
	}
	if count == 0 {
		if _, err := db.Exec(`
			ALTER TABLE customer_areas
			ADD COLUMN is_public BOOLEAN DEFAULT FALSE
		`); err != nil {
			return fmt.Errorf("add customer_areas.is_public column: %w", err)
		}
	}

	var idxCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_areas'
		AND INDEX_NAME = 'idx_customer_areas_is_public'`).Scan(&idxCount); err != nil {
		return fmt.Errorf("check customer_areas.is_public index: %w", err)
	}
	if idxCount == 0 {
		if _, err := db.Exec(`
			ALTER TABLE customer_areas
			ADD INDEX idx_customer_areas_is_public (is_public)
		`); err != nil {
			return fmt.Errorf("add customer_areas.is_public index: %w", err)
		}
	}
	return nil
}

func addIsPublicFieldToCustomerBrands(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_brands'
		AND COLUMN_NAME = 'is_public'`).Scan(&count); err != nil {
		return fmt.Errorf("check customer_brands.is_public column: %w", err)
	}
	if count == 0 {
		if _, err := db.Exec(`
			ALTER TABLE customer_brands
			ADD COLUMN is_public BOOLEAN DEFAULT FALSE
		`); err != nil {
			return fmt.Errorf("add customer_brands.is_public column: %w", err)
		}
	}

	var idxCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'customer_brands'
		AND INDEX_NAME = 'idx_customer_brands_is_public'`).Scan(&idxCount); err != nil {
		return fmt.Errorf("check customer_brands.is_public index: %w", err)
	}
	if idxCount == 0 {
		if _, err := db.Exec(`
			ALTER TABLE customer_brands
			ADD INDEX idx_customer_brands_is_public (is_public)
		`); err != nil {
			return fmt.Errorf("add customer_brands.is_public index: %w", err)
		}
	}
	return nil
}
