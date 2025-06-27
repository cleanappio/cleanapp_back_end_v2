package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"report-listener/config"
	"report-listener/models"

	_ "github.com/go-sql-driver/mysql"
)

// Database handles all database operations
type Database struct {
	db *sql.DB
}

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.Config) (*Database, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Printf("Database connected successfully to %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// GetReportsSince retrieves reports since a given sequence number
func (d *Database) GetReportsSince(ctx context.Context, sinceSeq int) ([]models.Report, error) {
	query := `
		SELECT seq, ts, id, team, latitude, longitude, x, y, image, action_id
		FROM reports
		WHERE seq > ?
		ORDER BY seq ASC
	`

	rows, err := d.db.QueryContext(ctx, query, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer rows.Close()

	var reports []models.Report
	for rows.Next() {
		var report models.Report
		err := rows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&report.X,
			&report.Y,
			&report.Image,
			&report.ActionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	return reports, nil
}

// GetLatestReportSeq returns the latest sequence number from the reports table
func (d *Database) GetLatestReportSeq(ctx context.Context) (int, error) {
	var seq int
	err := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) FROM reports").Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest report seq: %w", err)
	}
	return seq, nil
}

// GetReportCount returns the total number of reports
func (d *Database) GetReportCount(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get report count: %w", err)
	}
	return count, nil
}

// GetLastProcessedSeq retrieves the last processed sequence number from persistent storage
func (d *Database) GetLastProcessedSeq(ctx context.Context) (int, error) {
	var seq int
	err := d.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(last_processed_seq), 0) FROM service_state WHERE service_name = 'report-listener'").Scan(&seq)
	if err != nil {
		// If table doesn't exist or no record found, return 0
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get last processed seq: %w", err)
	}
	return seq, nil
}

// UpdateLastProcessedSeq updates the last processed sequence number in persistent storage
func (d *Database) UpdateLastProcessedSeq(ctx context.Context, seq int) error {
	// Use UPSERT to handle both insert and update
	query := `
		INSERT INTO service_state (service_name, last_processed_seq, updated_at) 
		VALUES ('report-listener', ?, NOW())
		ON DUPLICATE KEY UPDATE 
			last_processed_seq = VALUES(last_processed_seq),
			updated_at = NOW()
	`

	_, err := d.db.ExecContext(ctx, query, seq)
	if err != nil {
		return fmt.Errorf("failed to update last processed seq: %w", err)
	}

	return nil
}

// EnsureServiceStateTable creates the service_state table if it doesn't exist
func (d *Database) EnsureServiceStateTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS service_state (
			service_name VARCHAR(100) PRIMARY KEY,
			last_processed_seq INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_service_name (service_name)
		)
	`

	_, err := d.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create service_state table: %w", err)
	}

	return nil
}
