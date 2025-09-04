package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"

	"report_processor/config"
	"report_processor/models"

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

// EnsureReportStatusTable creates the report_status table if it doesn't exist
func (d *Database) EnsureReportStatusTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS report_status (
			seq INT NOT NULL,
			status ENUM('active', 'resolved') NOT NULL DEFAULT 'active',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (seq),
			FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE
		)
	`

	_, err := d.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create report_status table: %w", err)
	}

	log.Println("Report status table ensured")
	return nil
}

// MarkReportResolved marks a report as resolved
func (d *Database) MarkReportResolved(ctx context.Context, seq int) error {
	// First check if the report exists in the reports table
	var exists int
	err := d.db.QueryRowContext(ctx, "SELECT 1 FROM reports WHERE seq = ?", seq).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("report with seq %d does not exist", seq)
		}
		return fmt.Errorf("failed to check if report exists: %w", err)
	}

	// Use INSERT ... ON DUPLICATE KEY UPDATE to insert or update
	query := `
		INSERT INTO report_status (seq, status, updated_at) 
		VALUES (?, 'resolved', NOW())
		ON DUPLICATE KEY UPDATE 
			status = 'resolved',
			updated_at = NOW()
	`

	_, err = d.db.ExecContext(ctx, query, seq)
	if err != nil {
		return fmt.Errorf("failed to mark report as resolved: %w", err)
	}

	log.Printf("Report %d marked as resolved", seq)
	return nil
}

// GetReportStatus gets the status of a report
func (d *Database) GetReportStatus(ctx context.Context, seq int) (*models.ReportStatus, error) {
	query := `
		SELECT seq, status, created_at, updated_at
		FROM report_status
		WHERE seq = ?
	`

	var status models.ReportStatus
	err := d.db.QueryRowContext(ctx, query, seq).Scan(
		&status.Seq,
		&status.Status,
		&status.CreatedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Report status doesn't exist
		}
		return nil, fmt.Errorf("failed to get report status: %w", err)
	}

	return &status, nil
}

// GetReportStatusCount gets the count of reports by status
func (d *Database) GetReportStatusCount(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM report_status
		GROUP BY status
	`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get report status count: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		err := rows.Scan(&status, &count)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report status count: %w", err)
		}
		counts[status] = count
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating report status counts: %w", err)
	}

	return counts, nil
}

// GetReportsInRadius gets reports within a specified radius (in meters) of a given point
func (d *Database) GetReportsInRadius(ctx context.Context, latitude, longitude float64, radiusMeters float64) ([]models.Report, error) {
	// Calculate bounding box coordinates
	// Convert radius from meters to degrees
	// 1 degree latitude ≈ 111,320 meters
	// 1 degree longitude ≈ 111,320 * cos(latitude) meters
	latRadiusDegrees := radiusMeters / 111320.0
	lonRadiusDegrees := radiusMeters / (111320.0 * math.Cos(latitude*math.Pi/180.0))

	minLat := latitude - latRadiusDegrees
	maxLat := latitude + latRadiusDegrees
	minLng := longitude - lonRadiusDegrees
	maxLng := longitude + lonRadiusDegrees

	query := `
		SELECT r.seq, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id
		FROM reports r
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.latitude BETWEEN ? AND ?
		AND r.longitude BETWEEN ? AND ?
		AND (rs.status IS NULL OR rs.status = 'active')
	`

	rows, err := d.db.QueryContext(ctx, query, minLat, maxLat, minLng, maxLng)
	if err != nil {
		return nil, fmt.Errorf("failed to get reports in radius: %w", err)
	}
	defer rows.Close()

	var reports []models.Report
	for rows.Next() {
		var report models.Report
		err := rows.Scan(
			&report.Seq,
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
