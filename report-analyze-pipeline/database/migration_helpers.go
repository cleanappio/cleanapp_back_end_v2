package database

import (
	"context"
	"database/sql"
	"fmt"
)

func createReportAnalysisTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS report_analysis (
			seq INT NOT NULL,
			source VARCHAR(255) NOT NULL,
			analysis_text TEXT,
			analysis_image LONGBLOB,
			title VARCHAR(500),
			description TEXT,
			brand_name VARCHAR(255) DEFAULT '',
			brand_display_name VARCHAR(255) DEFAULT '',
			litter_probability FLOAT,
			hazard_probability FLOAT,
			digital_bug_probability FLOAT DEFAULT 0.0,
			severity_level FLOAT,
			summary TEXT,
			language VARCHAR(2) NOT NULL DEFAULT 'en',
			is_valid BOOLEAN DEFAULT TRUE,
			classification ENUM('physical', 'digital') DEFAULT 'physical',
			inferred_contact_emails TEXT,
			legal_risk_estimate TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX seq_index (seq),
			INDEX source_index (source),
			INDEX idx_report_analysis_brand_name (brand_name),
			INDEX idx_report_analysis_brand_display_name (brand_display_name),
			INDEX idx_report_analysis_language (language),
			INDEX idx_report_analysis_is_valid (is_valid),
			INDEX idx_report_analysis_classification (classification),
			FULLTEXT INDEX ft_report (title, description, brand_name, brand_display_name, summary)
		)`)
	if err != nil {
		return fmt.Errorf("failed to create report_analysis table: %w", err)
	}
	return nil
}

func migrateReportAnalysisTable(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "is_valid", sql: "ALTER TABLE report_analysis ADD COLUMN is_valid BOOLEAN DEFAULT TRUE"},
		{name: "classification", sql: "ALTER TABLE report_analysis ADD COLUMN classification ENUM('physical', 'digital') DEFAULT 'physical'"},
		{name: "digital_bug_probability", sql: "ALTER TABLE report_analysis ADD COLUMN digital_bug_probability FLOAT DEFAULT 0.0"},
		{name: "inferred_contact_emails", sql: "ALTER TABLE report_analysis ADD COLUMN inferred_contact_emails TEXT"},
		{name: "legal_risk_estimate", sql: "ALTER TABLE report_analysis ADD COLUMN legal_risk_estimate TEXT"},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, db, "report_analysis", column.name, column.sql); err != nil {
			return err
		}
	}
	indexes := []struct {
		name string
		sql  string
	}{
		{name: "idx_report_analysis_is_valid", sql: "ALTER TABLE report_analysis ADD INDEX idx_report_analysis_is_valid (is_valid)"},
		{name: "idx_report_analysis_classification", sql: "ALTER TABLE report_analysis ADD INDEX idx_report_analysis_classification (classification)"},
		{name: "ft_report", sql: "ALTER TABLE report_analysis ADD FULLTEXT INDEX ft_report (title, description, brand_name, brand_display_name, summary)"},
	}
	for _, index := range indexes {
		if err := ensureIndex(ctx, db, "report_analysis", index.name, index.sql); err != nil {
			return err
		}
	}
	return nil
}

func createOSMCacheTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS osm_location_cache (
			id INT AUTO_INCREMENT PRIMARY KEY,
			lat_grid DOUBLE NOT NULL,
			lon_grid DOUBLE NOT NULL,
			location_context JSON NOT NULL,
			inferred_emails TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			UNIQUE KEY idx_lat_lon (lat_grid, lon_grid),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return fmt.Errorf("failed to create osm_location_cache table: %w", err)
	}
	return nil
}

func createBrandContactsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS brand_contacts (
			id INT AUTO_INCREMENT PRIMARY KEY,
			brand_name VARCHAR(255) NOT NULL,
			product_name VARCHAR(255),
			contact_name VARCHAR(255),
			contact_title VARCHAR(255),
			contact_level ENUM('ic', 'manager', 'director', 'vp', 'c_suite', 'founder') DEFAULT 'ic',
			email VARCHAR(255),
			email_verified BOOLEAN DEFAULT FALSE,
			twitter_handle VARCHAR(255),
			linkedin_url VARCHAR(512),
			github_handle VARCHAR(255),
			source ENUM('linkedin', 'website', 'github', 'twitter', 'manual', 'inferred') DEFAULT 'manual',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_brand_name (brand_name),
			INDEX idx_brand_product (brand_name, product_name),
			INDEX idx_contact_level (contact_level),
			UNIQUE KEY uk_brand_contact_email (brand_name, email)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return fmt.Errorf("failed to create brand_contacts table: %w", err)
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, tableName, columnName, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`, tableName, columnName).Scan(&count); err != nil {
		return fmt.Errorf("failed to check column %s.%s: %w", tableName, columnName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to alter %s.%s: %w", tableName, columnName, err)
	}
	return nil
}

func ensureIndex(ctx context.Context, db *sql.DB, tableName, indexName, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?
	`, tableName, indexName).Scan(&count); err != nil {
		return fmt.Errorf("failed to check index %s on %s: %w", indexName, tableName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add index %s on %s: %w", indexName, tableName, err)
	}
	return nil
}
