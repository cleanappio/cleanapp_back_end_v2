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

// EnsureResponsesTable creates the responses table if it doesn't exist
func (d *Database) EnsureResponsesTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS responses (
			seq INT NOT NULL AUTO_INCREMENT,
			ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			id VARCHAR(255) NOT NULL,
			team INT NOT NULL,
			latitude FLOAT NOT NULL,
			longitude FLOAT NOT NULL,
			x FLOAT,
			y FLOAT,
			image LONGBLOB NOT NULL,
			action_id VARCHAR(32),
			description VARCHAR(255),
			status ENUM('resolved', 'verified') NOT NULL DEFAULT 'resolved',
			report_seq INT NOT NULL,
			PRIMARY KEY (seq),
			INDEX id_index (id),
			INDEX action_idx (action_id),
			INDEX latitude_index (latitude),
			INDEX longitude_index (longitude),
			INDEX status_index (status),
			INDEX report_seq_index (report_seq),
			FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
		)
	`

	_, err := d.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create responses table: %w", err)
	}

	log.Println("Responses table ensured")
	return nil
}

// EnsureReportClustersTable creates the report_clusters table if it doesn't exist
func (d *Database) EnsureReportClustersTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS report_clusters(
			primary_seq INT NOT NULL,
			related_seq INT NOT NULL,
			PRIMARY KEY (primary_seq, related_seq),
			INDEX primary_seq_index (primary_seq),
			UNIQUE INDEX related_seq_unique (related_seq),
			FOREIGN KEY (primary_seq) REFERENCES reports(seq) ON DELETE CASCADE,
			FOREIGN KEY (related_seq) REFERENCES reports(seq) ON DELETE CASCADE
		)
	`

	_, err := d.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create report_clusters table: %w", err)
	}

	log.Println("Report clusters table ensured")
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

	log.Printf("latRadiusDegrees: %f, lonRadiusDegrees: %f", latRadiusDegrees, lonRadiusDegrees)

	minLat := latitude - latRadiusDegrees
	maxLat := latitude + latRadiusDegrees
	minLng := longitude - lonRadiusDegrees
	maxLng := longitude + lonRadiusDegrees

	log.Printf("minLat: %f, maxLat: %f, minLng: %f, maxLng: %f", minLat, maxLat, minLng, maxLng)

	query := `
		SELECT r.seq, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id, ra.description
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.latitude BETWEEN ? AND ?
		AND r.longitude BETWEEN ? AND ?
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ra.is_valid = TRUE
		AND ra.language = 'en'
		ORDER BY r.seq DESC
		LIMIT ?
	`

	rows, err := d.db.QueryContext(ctx, query, minLat, maxLat, minLng, maxLng, 10)
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
			&report.AnalysisText,
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

// GetResponse gets a response by seq
func (d *Database) GetResponse(ctx context.Context, seq int) (*models.Response, error) {
	query := `
		SELECT seq, id, team, latitude, longitude, x, y, image, action_id, status, report_seq
		FROM responses
		WHERE seq = ?
	`

	var response models.Response
	err := d.db.QueryRowContext(ctx, query, seq).Scan(
		&response.Seq,
		&response.ID,
		&response.Team,
		&response.Latitude,
		&response.Longitude,
		&response.X,
		&response.Y,
		&response.Image,
		&response.ActionID,
		&response.Status,
		&response.ReportSeq,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Response doesn't exist
		}
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	return &response, nil
}

// GetResponsesByStatus gets responses by status
func (d *Database) GetResponsesByStatus(ctx context.Context, status string) ([]models.Response, error) {
	query := `
		SELECT seq, id, team, latitude, longitude, x, y, image, action_id, status, report_seq
		FROM responses
		WHERE status = ?
		ORDER BY ts DESC
	`

	rows, err := d.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to get responses by status: %w", err)
	}
	defer rows.Close()

	var responses []models.Response
	for rows.Next() {
		var response models.Response
		err := rows.Scan(
			&response.Seq,
			&response.ID,
			&response.Team,
			&response.Latitude,
			&response.Longitude,
			&response.X,
			&response.Y,
			&response.Image,
			&response.ActionID,
			&response.Status,
			&response.ReportSeq,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan response: %w", err)
		}
		responses = append(responses, response)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating responses: %w", err)
	}

	return responses, nil
}

// CreateResponseFromMatchRequest creates a response entry from match request data and increments user's kitn_daily
func (d *Database) CreateResponseFromMatchRequest(ctx context.Context, req models.MatchReportRequest, reportSeq int, status string) (*models.Response, error) {
	// Start a transaction to ensure both operations succeed or fail together
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback will be ignored if tx.Commit() is called

	// Create response entry directly from match request data
	insertQuery := `
		INSERT INTO responses (id, team, latitude, longitude, x, y, image, action_id, status, report_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := tx.ExecContext(ctx, insertQuery,
		req.ID,
		0, // team - not available in match request, default to 0 (UNKNOWN)
		req.Latitude,
		req.Longitude,
		req.X,
		req.Y,
		req.Image,
		nil, // action_id - not available in match request
		status,
		reportSeq, // Reference to the resolved report
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create response from match request: %w", err)
	}

	responseSeq, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get response seq: %w", err)
	}

	// Update user's kitn_daily field by incrementing it by 1
	updateQuery := `
		UPDATE users 
		SET kitns_daily = kitns_daily + 1 
		WHERE id = ?
	`

	_, err = tx.ExecContext(ctx, updateQuery, req.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to increment user kitn_daily: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Return the created response
	response := &models.Response{
		Seq:       int(responseSeq),
		ID:        req.ID,
		Team:      0, // UNKNOWN team
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		X:         req.X,
		Y:         req.Y,
		Image:     req.Image,
		ActionID:  nil, // Not available in match request
		Status:    status,
		ReportSeq: reportSeq, // Reference to the resolved report
	}

	log.Printf("Response created with seq %d from match request for report %d with status %s, user %s kitn_daily incremented", responseSeq, reportSeq, status, req.ID)
	return response, nil
}

// InsertReportCluster inserts a new report cluster relationship
func (d *Database) InsertReportCluster(ctx context.Context, primarySeq, relatedSeq int) error {
	query := `
		INSERT INTO report_clusters (primary_seq, related_seq)
		VALUES (?, ?)
	`

	_, err := d.db.ExecContext(ctx, query, primarySeq, relatedSeq)
	if err != nil {
		return fmt.Errorf("failed to insert report cluster: %w", err)
	}

	log.Printf("Report cluster created: primary_seq=%d, related_seq=%d", primarySeq, relatedSeq)
	return nil
}

// GetLatestReportSeq gets the latest auto-incremented sequence number from the reports table
func (d *Database) GetLatestReportSeq(ctx context.Context) (int, error) {
	query := `SELECT seq FROM reports ORDER BY seq DESC LIMIT 1`

	var seq int
	err := d.db.QueryRowContext(ctx, query).Scan(&seq)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // No reports exist yet
		}
		return 0, fmt.Errorf("failed to get latest report seq: %w", err)
	}

	return seq, nil
}
