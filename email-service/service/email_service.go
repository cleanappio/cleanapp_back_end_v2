package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"email-service/config"
	"email-service/email"
	"email-service/models"

	"github.com/apex/log"
	_ "github.com/go-sql-driver/mysql"
	geojson "github.com/paulmach/go.geojson"
)

// EmailService handles the email sending logic
type EmailService struct {
	db     *sql.DB
	config *config.Config
	email  *email.EmailSender
}

// NewEmailService creates a new email service
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	// Connect to database
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Verify and create required tables
	if err := verifyAndCreateTables(db); err != nil {
		return nil, fmt.Errorf("failed to verify/create tables: %w", err)
	}

	// Create email sender
	emailSender := email.NewEmailSender(cfg)

	return &EmailService{
		db:     db,
		config: cfg,
		email:  emailSender,
	}, nil
}

// Close closes the database connection
func (s *EmailService) Close() error {
	return s.db.Close()
}

// ProcessReports polls for new reports and sends emails
func (s *EmailService) ProcessReports() error {
	ctx := context.Background()

	// Get reports that haven't been processed for email sending
	reports, err := s.getUnprocessedReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to get unprocessed reports: %w", err)
	}

	log.Infof("Found %d unprocessed reports", len(reports))

	for _, report := range reports {
		if err := s.processReport(ctx, report); err != nil {
			log.Errorf("Failed to process report %d: %v", report.Seq, err)
			continue
		}
	}

	return nil
}

// getUnprocessedReports gets reports that have been analyzed but haven't been sent emails for
func (s *EmailService) getUnprocessedReports(ctx context.Context) ([]models.Report, error) {
	query := `
		SELECT r.seq, r.id, r.latitude, r.longitude, r.image, r.ts
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
		WHERE sre.seq IS NULL
		ORDER BY r.ts ASC
		LIMIT 100
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []models.Report
	for rows.Next() {
		var report models.Report
		if err := rows.Scan(&report.Seq, &report.ID, &report.Latitude, &report.Longitude, &report.Image, &report.Timestamp); err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}

	return reports, nil
}

// processReport processes a single report and sends emails if needed
func (s *EmailService) processReport(ctx context.Context, report models.Report) error {
	// Get analysis data for this report
	analysis, err := s.getReportAnalysis(ctx, report.Seq)
	if err != nil {
		return fmt.Errorf("failed to get analysis for report %d: %w", report.Seq, err)
	}

	// Find areas that contain this report point
	features, emails, err := s.findAreasForReport(ctx, report)
	if err != nil {
		return fmt.Errorf("failed to find areas for report: %w", err)
	}

	// If no areas found, mark as processed and return
	if len(emails) == 0 {
		return s.markReportAsProcessed(ctx, report.Seq)
	}

	// Send emails for each area
	for areaID, emailAddrs := range emails {
		if err := s.sendEmailsForArea(ctx, report, analysis, features[areaID], emailAddrs); err != nil {
			log.Errorf("Failed to send emails for area %d: %v", areaID, err)
			// Continue with other areas
		}
	}

	// Mark report as processed
	return s.markReportAsProcessed(ctx, report.Seq)
}

// findAreasForReport finds areas that contain the report point and their associated emails
func (s *EmailService) findAreasForReport(ctx context.Context, report models.Report) (map[uint64]*geojson.Feature, map[uint64][]string, error) {
	// Convert point to WKT format
	ptWKT := fmt.Sprintf("POINT(%g %g)", report.Longitude, report.Latitude)

	// Find areas that contain this point
	rows, err := s.db.QueryContext(ctx,
		"SELECT area_id FROM area_index WHERE MBRWithin(ST_GeomFromText(?, 4326), geom)",
		ptWKT)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	areaMap := make(map[uint64]bool)
	for rows.Next() {
		var areaID uint64
		if err := rows.Scan(&areaID); err != nil {
			return nil, nil, err
		}
		areaMap[areaID] = true
	}

	if len(areaMap) == 0 {
		return nil, nil, nil
	}

	// Get area features
	areaFeatures, err := s.getAreaFeatures(ctx, areaMap)
	if err != nil {
		return nil, nil, err
	}

	// Get emails for areas
	areaEmails, err := s.getAreaEmails(ctx, areaMap)
	if err != nil {
		return nil, nil, err
	}

	return areaFeatures, areaEmails, nil
}

// getAreaFeatures gets the GeoJSON features for the given areas
func (s *EmailService) getAreaFeatures(ctx context.Context, areaMap map[uint64]bool) (map[uint64]*geojson.Feature, error) {
	if len(areaMap) == 0 {
		return nil, nil
	}

	areaIDs := make([]any, 0, len(areaMap))
	for areaID := range areaMap {
		areaIDs = append(areaIDs, areaID)
	}

	placeholders := strings.Repeat("?,", len(areaIDs))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT id, area_json FROM areas WHERE id IN (%s)", placeholders)
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	features := make(map[uint64]*geojson.Feature)
	for rows.Next() {
		var areaID uint64
		var areaJSON string
		if err := rows.Scan(&areaID, &areaJSON); err != nil {
			return nil, err
		}

		feature := &geojson.Feature{}
		if err := json.Unmarshal([]byte(areaJSON), feature); err != nil {
			return nil, err
		}
		features[areaID] = feature
	}

	return features, nil
}

// getAreaEmails gets the email addresses for the given areas
func (s *EmailService) getAreaEmails(ctx context.Context, areaMap map[uint64]bool) (map[uint64][]string, error) {
	if len(areaMap) == 0 {
		return nil, nil
	}

	areaIDs := make([]any, 0, len(areaMap))
	for areaID := range areaMap {
		areaIDs = append(areaIDs, areaID)
	}

	placeholders := strings.Repeat("?,", len(areaIDs))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

	query := fmt.Sprintf("SELECT area_id, email FROM contact_emails WHERE area_id IN (%s) AND consent_report = true", placeholders)
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	areaEmails := make(map[uint64][]string)
	for rows.Next() {
		var areaID uint64
		var email string
		if err := rows.Scan(&areaID, &email); err != nil {
			return nil, err
		}

		if areaEmails[areaID] == nil {
			areaEmails[areaID] = []string{}
		}
		areaEmails[areaID] = append(areaEmails[areaID], email)
	}

	return areaEmails, nil
}

// sendEmailsForArea sends emails for a specific area
func (s *EmailService) sendEmailsForArea(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, feature *geojson.Feature, emails []string) error {
	if len(emails) == 0 {
		return nil
	}

	// Generate polygon image
	polyImg, err := s.email.GeneratePolygonImage(feature, report.Latitude, report.Longitude)
	if err != nil {
		return fmt.Errorf("failed to generate polygon image: %w", err)
	}

	// Send emails with analysis data
	return s.email.SendEmailsWithAnalysis(emails, report.Image, polyImg, analysis)
}

// getReportAnalysis gets the analysis data for a specific report
func (s *EmailService) getReportAnalysis(ctx context.Context, seq int64) (*models.ReportAnalysis, error) {
	query := `
		SELECT seq, source, title, description, litter_probability, hazard_probability, severity_level, summary
		FROM report_analysis
		WHERE seq = ?
		LIMIT 1
	`

	var analysis models.ReportAnalysis
	err := s.db.QueryRowContext(ctx, query, seq).Scan(
		&analysis.Seq,
		&analysis.Source,
		&analysis.Title,
		&analysis.Description,
		&analysis.LitterProbability,
		&analysis.HazardProbability,
		&analysis.SeverityLevel,
		&analysis.Summary,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get analysis for seq %d: %w", seq, err)
	}

	return &analysis, nil
}

// markReportAsProcessed marks a report as processed for email sending
func (s *EmailService) markReportAsProcessed(ctx context.Context, seq int64) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO sent_reports_emails (seq) VALUES (?)", seq)
	return err
}

// verifyAndCreateTables ensures all required tables exist with proper structure
func verifyAndCreateTables(db *sql.DB) error {
	ctx := context.Background()

	// Check if sent_reports_emails table exists
	var tableExists int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'sent_reports_emails'
	`).Scan(&tableExists)

	if err != nil {
		return fmt.Errorf("failed to check if sent_reports_emails table exists: %w", err)
	}

	if tableExists == 0 {
		log.Info("Creating sent_reports_emails table...")

		// Create the table with proper indexing
		createTableSQL := `
			CREATE TABLE sent_reports_emails (
				seq INT PRIMARY KEY,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				INDEX idx_created_at (created_at),
				INDEX idx_seq (seq)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create sent_reports_emails table: %w", err)
		}

		log.Info("sent_reports_emails table created successfully")
	} else {
		log.Info("sent_reports_emails table already exists")

		// Verify that the seq index exists
		var indexExists int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM information_schema.statistics 
			WHERE table_schema = DATABASE() 
			AND table_name = 'sent_reports_emails' 
			AND index_name = 'PRIMARY'
		`).Scan(&indexExists)

		if err != nil {
			return fmt.Errorf("failed to check seq index: %w", err)
		}

		if indexExists == 0 {
			log.Warn("seq index missing on sent_reports_emails table, this may cause performance issues")
		} else {
			log.Info("seq index verified on sent_reports_emails table")
		}
	}

	// Verify that required tables exist
	requiredTables := []string{"reports", "area_index", "areas", "contact_emails", "report_analysis"}
	for _, tableName := range requiredTables {
		var exists int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) 
			FROM information_schema.tables 
			WHERE table_schema = DATABASE() 
			AND table_name = ?
		`, tableName).Scan(&exists)

		if err != nil {
			return fmt.Errorf("failed to check if %s table exists: %w", tableName, err)
		}

		if exists == 0 {
			return fmt.Errorf("required table %s does not exist", tableName)
		}
	}

	log.Info("All required tables verified")
	return nil
}
