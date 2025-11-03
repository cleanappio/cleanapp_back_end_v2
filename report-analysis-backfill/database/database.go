package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"report-analysis-backfill/config"
	"report-analysis-backfill/models"

	_ "github.com/go-sql-driver/mysql"
)

// Database represents the database connection
type Database struct {
	db *sql.DB
}

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.Config) (*Database, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection with exponential backoff retry
	var waitInterval time.Duration = 1 * time.Second
	for {
		if err := db.Ping(); err == nil {
			break // Connection successful
		}
		log.Printf("Database connection failed, retrying in %v: %v", waitInterval, err)
		time.Sleep(waitInterval)
		waitInterval *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, ...
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// GetUnanalyzedReports gets reports that haven't been analyzed yet (without images)
func (d *Database) GetUnanalyzedReports(cfg *config.Config, limit int) ([]models.Report, error) {
	query := `
	SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id, r.description
	FROM reports r
	WHERE r.seq NOT IN(SELECT seq FROM report_analysis) AND r.seq < ?
	ORDER BY r.seq ASC
	LIMIT ?`

	rows, err := d.db.Query(query, cfg.SeqEndTo, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unanalyzed reports: %w", err)
	}
	defer rows.Close()

	var description sql.NullString
	var actionID sql.NullString
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
			&actionID,
			&description,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		report.Description = description.String
		report.ActionID = actionID.String
		// Image will be fetched separately when needed
		reports = append(reports, report)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	log.Printf("Found %d unanalyzed reports", len(reports))
	return reports, nil
}

// GetReportImage gets the image for a specific report by sequence number
func (d *Database) GetReportImage(seq int) ([]byte, error) {
	query := `SELECT r.image FROM reports r WHERE r.seq = ?`

	var imageData []byte
	err := d.db.QueryRow(query, seq).Scan(&imageData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found", seq)
		}
		return nil, fmt.Errorf("failed to get image for report seq %d: %w", seq, err)
	}

	return imageData, nil
}

// GetLastProcessedSeq gets the last processed sequence number
func (d *Database) GetLastProcessedSeq() (int, error) {
	query := `SELECT MAX(seq) FROM report_analysis`

	var lastSeq sql.NullInt64
	err := d.db.QueryRow(query).Scan(&lastSeq)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // No analyses yet
		}
		return 0, fmt.Errorf("failed to get last processed seq: %w", err)
	}

	if lastSeq.Valid {
		return int(lastSeq.Int64), nil
	}
	return 0, nil
}

// GetDB returns the underlying sql.DB for direct queries
func (d *Database) GetDB() *sql.DB {
	return d.db
}
