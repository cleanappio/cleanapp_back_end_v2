package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"report-analyze-pipeline/config"

	_ "github.com/go-sql-driver/mysql"
)

// Database represents the database connection
type Database struct {
	db *sql.DB
}

// Report represents a report from the reports table
type Report struct {
	Seq       int
	Timestamp time.Time
	ID        string
	Team      int
	Latitude  float64
	Longitude float64
	X         *float64
	Y         *float64
	Image     []byte
	ActionID  *string
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq               int
	Source            string
	AnalysisText      string
	AnalysisImage     []byte
	Title             string
	Description       string
	BrandName         string
	BrandDisplayName  string
	LitterProbability float64
	HazardProbability float64
	SeverityLevel     float64
	Summary           string
	Language          string
	IsValid           bool
}

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.Config) (*Database, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
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

// CreateReportAnalysisTable creates the report_analysis table if it doesn't exist
func (d *Database) CreateReportAnalysisTable() error {
	query := `
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
		severity_level FLOAT,
		summary TEXT,
		language VARCHAR(2) NOT NULL DEFAULT 'en',
		is_valid BOOLEAN DEFAULT TRUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX seq_index (seq),
		INDEX source_index (source),
		INDEX idx_report_analysis_brand_name (brand_name),
		INDEX idx_report_analysis_brand_display_name (brand_display_name),
		INDEX idx_report_analysis_language (language),
		INDEX idx_report_analysis_is_valid (is_valid)
	)`

	_, err := d.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create report_analysis table: %w", err)
	}

	log.Println("report_analysis table created/verified successfully")
	return nil
}

// columnExists checks if a column exists in a table
func (d *Database) columnExists(tableName, columnName string) (bool, error) {
	query := `
	SELECT COUNT(*) 
	FROM INFORMATION_SCHEMA.COLUMNS 
	WHERE TABLE_SCHEMA = DATABASE() 
	AND TABLE_NAME = ? 
	AND COLUMN_NAME = ?`

	var count int
	err := d.db.QueryRow(query, tableName, columnName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if column exists: %w", err)
	}

	return count > 0, nil
}

// indexExists checks if an index exists in a table
func (d *Database) indexExists(tableName, indexName string) (bool, error) {
	query := `
	SELECT COUNT(*) 
	FROM INFORMATION_SCHEMA.STATISTICS 
	WHERE TABLE_SCHEMA = DATABASE() 
	AND TABLE_NAME = ? 
	AND INDEX_NAME = ?`

	var count int
	err := d.db.QueryRow(query, tableName, indexName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if index exists: %w", err)
	}

	return count > 0, nil
}

func (d *Database) MigrateReportAnalysisTable() error {
	// Check if is_valid column already exists
	isValidExists, err := d.columnExists("report_analysis", "is_valid")
	if err != nil {
		return fmt.Errorf("failed to check if is_valid column exists: %w", err)
	}

	if !isValidExists {
		log.Println("Adding is_valid column to report_analysis table...")

		query := `ALTER TABLE report_analysis ADD COLUMN is_valid BOOLEAN DEFAULT TRUE`
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add is_valid column: %w", err)
		}

		log.Println("Successfully added is_valid column to report_analysis table")
	} else {
		log.Println("is_valid column already exists in report_analysis table, skipping migration")
	}

	// Check if is_valid index already exists
	isValidIndexExists, err := d.indexExists("report_analysis", "idx_report_analysis_is_valid")
	if err != nil {
		return fmt.Errorf("failed to check if is_valid index exists: %w", err)
	}

	if !isValidIndexExists {
		log.Println("Adding index for is_valid column to report_analysis table...")

		indexQuery := `ALTER TABLE report_analysis ADD INDEX idx_report_analysis_is_valid (is_valid)`
		_, err = d.db.Exec(indexQuery)
		if err != nil {
			return fmt.Errorf("failed to add is_valid index: %w", err)
		}

		log.Println("Successfully added index for is_valid column to report_analysis table")
	} else {
		log.Println("is_valid index already exists in report_analysis table, skipping migration")
	}

	return nil
}

// GetUnanalyzedReports gets reports that haven't been analyzed yet
func (d *Database) GetUnanalyzedReports(cfg *config.Config, limit int) ([]Report, error) {
	query := `
	SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id
	FROM reports r
	LEFT JOIN report_analysis ra ON r.seq = ra.seq
	WHERE ra.seq IS NULL AND r.seq > ?
	ORDER BY r.seq ASC
	LIMIT ?`

	rows, err := d.db.Query(query, cfg.SeqStartFrom, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unanalyzed reports: %w", err)
	}
	defer rows.Close()

	var reports []Report
	for rows.Next() {
		var report Report
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

	return reports, nil
}

// SaveAnalysis saves the analysis result to the database
func (d *Database) SaveAnalysis(analysis *ReportAnalysis) error {
	query := `
	INSERT INTO report_analysis (
		seq, source, analysis_text, analysis_image, 
		title, description, brand_name, brand_display_name,
		litter_probability, hazard_probability, severity_level, summary, language, is_valid
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.Exec(query,
		analysis.Seq,
		analysis.Source,
		analysis.AnalysisText,
		analysis.AnalysisImage,
		analysis.Title,
		analysis.Description,
		analysis.BrandName,
		analysis.BrandDisplayName,
		analysis.LitterProbability,
		analysis.HazardProbability,
		analysis.SeverityLevel,
		analysis.Summary,
		analysis.Language,
		analysis.IsValid,
	)
	if err != nil {
		return fmt.Errorf("failed to save analysis: %w", err)
	}

	return nil
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
