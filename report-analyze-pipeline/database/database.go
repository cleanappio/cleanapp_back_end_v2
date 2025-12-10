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
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	ID          string    `json:"id"`
	Team        int       `json:"team"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	Image       []byte    `json:"image"`
	ActionID    string    `json:"action_id"`
	Description string    `json:"description"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq                   int
	Source                string
	AnalysisText          string
	AnalysisImage         []byte
	Title                 string
	Description           string
	BrandName             string
	BrandDisplayName      string
	LitterProbability     float64
	HazardProbability     float64
	DigitalBugProbability float64
	SeverityLevel         float64
	Summary               string
	Language              string
	IsValid               bool
	Classification        string
	InferredContactEmails string
	LegalRiskEstimate     string
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
	// Check and add is_valid column
	exists, err := d.columnExists("report_analysis", "is_valid")
	if err != nil {
		return fmt.Errorf("failed to check if is_valid column exists: %w", err)
	}

	if !exists {
		log.Printf("Adding is_valid column to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD COLUMN is_valid BOOLEAN DEFAULT TRUE"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add is_valid column: %w", err)
		}
		log.Printf("Successfully added is_valid column to report_analysis table")
	} else {
		log.Printf("is_valid column already exists in report_analysis table, skipping migration")
	}

	// Check and add classification column
	exists, err = d.columnExists("report_analysis", "classification")
	if err != nil {
		return fmt.Errorf("failed to check if classification column exists: %w", err)
	}

	if !exists {
		log.Printf("Adding classification column to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD COLUMN classification ENUM('physical', 'digital') DEFAULT 'physical'"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add classification column: %w", err)
		}
		log.Printf("Successfully added classification column to report_analysis table")
	} else {
		log.Printf("classification column already exists in report_analysis table, skipping migration")
	}

	// Check and add digital_bug_probability column
	exists, err = d.columnExists("report_analysis", "digital_bug_probability")
	if err != nil {
		return fmt.Errorf("failed to check if digital_bug_probability column exists: %w", err)
	}

	if !exists {
		log.Printf("Adding digital_bug_probability column to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD COLUMN digital_bug_probability FLOAT DEFAULT 0.0"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add digital_bug_probability column: %w", err)
		}
		log.Printf("Successfully added digital_bug_probability column to report_analysis table")
	} else {
		log.Printf("digital_bug_probability column already exists in report_analysis table, skipping migration")
	}

	// Check and add inferred_contact_emails column
	exists, err = d.columnExists("report_analysis", "inferred_contact_emails")
	if err != nil {
		return fmt.Errorf("failed to check if inferred_contact_emails column exists: %w", err)
	}

	if !exists {
		log.Printf("Adding inferred_contact_emails column to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD COLUMN inferred_contact_emails TEXT"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add inferred_contact_emails column: %w", err)
		}
		log.Printf("Successfully added inferred_contact_emails column to report_analysis table")
	} else {
		log.Printf("inferred_contact_emails column already exists in report_analysis table, skipping migration")
	}

	// Check and add legal_risk_estimate column
	exists, err = d.columnExists("report_analysis", "legal_risk_estimate")
	if err != nil {
		return fmt.Errorf("failed to check if legal_risk_estimate column exists: %w", err)
	}

	if !exists {
		log.Printf("Adding legal_risk_estimate column to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD COLUMN legal_risk_estimate TEXT"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add legal_risk_estimate column: %w", err)
		}
		log.Printf("Successfully added legal_risk_estimate column to report_analysis table")
	} else {
		log.Printf("legal_risk_estimate column already exists in report_analysis table, skipping migration")
	}

	// Add indexes
	fields := []string{"is_valid", "classification"}
	for _, field := range fields {
		indexName := fmt.Sprintf("idx_report_analysis_%s", field)
		exists, err = d.indexExists("report_analysis", indexName)
		if err != nil {
			return fmt.Errorf("failed to check if %s index exists: %w", indexName, err)
		}

		if !exists {
			log.Printf("Adding %s index to report_analysis table...", indexName)
			query := fmt.Sprintf("ALTER TABLE report_analysis ADD INDEX %s (%s)", indexName, field)
			_, err = d.db.Exec(query)
			if err != nil {
				return fmt.Errorf("failed to add %s index: %w", indexName, err)
			}
			log.Printf("Successfully added %s index to report_analysis table", indexName)
		} else {
			log.Printf("%s index already exists in report_analysis table, skipping migration", indexName)
		}
	}

	// Check and add FULLTEXT index
	exists, err = d.indexExists("report_analysis", "ft_report")
	if err != nil {
		return fmt.Errorf("failed to check if ft_report index exists: %w", err)
	}

	if !exists {
		log.Printf("Adding ft_report FULLTEXT index to report_analysis table...")
		query := "ALTER TABLE report_analysis ADD FULLTEXT INDEX ft_report (title, description, brand_name, brand_display_name, summary)"
		_, err = d.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to add ft_report FULLTEXT index: %w", err)
		}
		log.Printf("Successfully added ft_report FULLTEXT index to report_analysis table")
	} else {
		log.Printf("ft_report FULLTEXT index already exists in report_analysis table, skipping migration")
	}

	return nil
}

// GetUnanalyzedReports gets reports that haven't been analyzed yet
func (d *Database) GetUnanalyzedReports(cfg *config.Config, limit int) ([]Report, error) {
	query := `
	SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id, r.description
	FROM reports r
	WHERE r.seq NOT IN(SELECT seq FROM report_analysis) AND r.seq > ?
	ORDER BY r.seq ASC
	LIMIT ?`
	// query := `
	// SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id, r.description
	// FROM reports r
	// INNER JOIN reports_gdpr rg ON r.seq = rg.seq
	// LEFT JOIN report_analysis ra ON r.seq = ra.seq
	// WHERE ra.seq IS NULL AND r.seq > ?
	// ORDER BY r.seq ASC
	// LIMIT ?`

	rows, err := d.db.Query(query, cfg.SeqStartFrom, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unanalyzed reports: %w", err)
	}
	defer rows.Close()

	var description sql.NullString

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
			&description,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		report.Description = description.String
		reports = append(reports, report)
	}

	return reports, nil
}

// GetReportBySeq gets a single report by sequence number
func (d *Database) GetReportBySeq(seq int) (*Report, error) {
	query := `
	SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id, r.description
	FROM reports r
	WHERE r.seq = ?`

	var report Report
	var description sql.NullString

	err := d.db.QueryRow(query, seq).Scan(
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
		&description,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found", seq)
		}
		return nil, fmt.Errorf("failed to fetch report %d: %w", seq, err)
	}

	report.Description = description.String
	return &report, nil
}

// GetReportImage gets only the image data for a report by sequence number
func (d *Database) GetReportImage(seq int) ([]byte, error) {
	query := `SELECT r.image FROM reports r WHERE r.seq = ?`

	var image []byte
	err := d.db.QueryRow(query, seq).Scan(&image)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found", seq)
		}
		return nil, fmt.Errorf("failed to fetch image for report %d: %w", seq, err)
	}

	return image, nil
}

// SaveAnalysis saves the analysis result to the database
func (d *Database) SaveAnalysis(analysis *ReportAnalysis) error {
	query := `
	INSERT INTO report_analysis (
		seq, source, analysis_text, analysis_image, 
		title, description, brand_name, brand_display_name,
		litter_probability, hazard_probability, digital_bug_probability,
		severity_level, summary, language, is_valid, classification, 
		inferred_contact_emails, legal_risk_estimate
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
		analysis.DigitalBugProbability,
		analysis.SeverityLevel,
		analysis.Summary,
		analysis.Language,
		analysis.IsValid,
		analysis.Classification,
		analysis.InferredContactEmails,
		analysis.LegalRiskEstimate,
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

// UpdateInferredContactEmails updates the inferred_contact_emails field for a report's English analysis
func (d *Database) UpdateInferredContactEmails(seq int, emails string) error {
	query := `
	UPDATE report_analysis 
	SET inferred_contact_emails = ?, updated_at = NOW()
	WHERE seq = ? AND language = 'en'`

	result, err := d.db.Exec(query, emails, seq)
	if err != nil {
		return fmt.Errorf("failed to update inferred_contact_emails for seq %d: %w", seq, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("no English analysis found for seq %d", seq)
	}

	log.Printf("Updated inferred_contact_emails for seq %d (%d rows)", seq, rows)
	return nil
}

// DigitalReportForEnrichment represents a digital report that needs contact enrichment
type DigitalReportForEnrichment struct {
	Seq       int
	BrandName string
}

// GetDigitalReportsNeedingEnrichment returns digital reports missing inferred_contact_emails
func (d *Database) GetDigitalReportsNeedingEnrichment(limit int) ([]DigitalReportForEnrichment, error) {
	query := `
	SELECT seq, brand_name 
	FROM report_analysis 
	WHERE classification = 'digital' 
	  AND language = 'en'
	  AND brand_name IS NOT NULL AND brand_name != ''
	  AND (inferred_contact_emails IS NULL OR inferred_contact_emails = '')
	ORDER BY seq DESC
	LIMIT ?`

	rows, err := d.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query digital reports needing enrichment: %w", err)
	}
	defer rows.Close()

	var reports []DigitalReportForEnrichment
	for rows.Next() {
		var r DigitalReportForEnrichment
		if err := rows.Scan(&r.Seq, &r.BrandName); err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, r)
	}

	return reports, nil
}

