package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

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

// isValidEmail checks if a string is a valid email address
func (s *EmailService) isValidEmail(email string) bool {
	// Updated regex to prevent consecutive dots and ensure proper email format
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// isEmailOptedOut checks if an email address has opted out from receiving emails
func (s *EmailService) isEmailOptedOut(ctx context.Context, email string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM opted_out_emails 
		WHERE email = ?
	`, email).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check if email %s is opted out: %w", email, err)
	}

	return count > 0, nil
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

	// Test connection with exponential backoff retry
	var waitInterval time.Duration = 1 * time.Second
	for {
		if err := db.Ping(); err == nil {
			break // Connection successful
		}
		log.WithError(err).Warnf("Database connection failed, retrying in %v", waitInterval)
		time.Sleep(waitInterval)
		waitInterval *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, ...
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
	start := time.Now()
	log.Info("Polling cycle started: fetching unprocessed reports")

	// Get reports that haven't been processed for email sending
	reports, err := s.getUnprocessedReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to get unprocessed reports: %w", err)
	}

	log.Infof("Found %d unprocessed reports (in %s)", len(reports), time.Since(start))

	reportsStart := time.Now()
	for _, report := range reports {
		if err := s.processReport(ctx, report); err != nil {
			log.Errorf("Failed to process report %d: %v", report.Seq, err)
			continue
		}
	}
	log.Infof("Finished processing %d reports (took %s)", len(reports), time.Since(reportsStart))
	return nil
}

// getUnprocessedReports gets reports that have been analyzed but haven't been sent emails for
func (s *EmailService) getUnprocessedReports(ctx context.Context) ([]models.Report, error) {
	qStart := time.Now()
	query := `
        SELECT r.seq, r.id, r.latitude, r.longitude, r.image, r.ts
        FROM (
            SELECT seq
            FROM reports
            ORDER BY seq DESC
            LIMIT 1000
        ) AS recent
        JOIN reports r ON r.seq = recent.seq
        INNER JOIN report_analysis ra ON r.seq = ra.seq
        LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
        WHERE sre.seq IS NULL
        AND ra.language = 'en'
        ORDER BY r.seq DESC
        LIMIT 100
    `

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Errorf("getUnprocessedReports query error (in %s): %v", time.Since(qStart), err)
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

	log.Infof("getUnprocessedReports returned %d rows (in %s)", len(reports), time.Since(qStart))
	return reports, nil
}

// processReport processes a single report and sends emails if needed
func (s *EmailService) processReport(ctx context.Context, report models.Report) error {
	// Get analysis data for this report
	analysis, err := s.getReportAnalysis(ctx, report.Seq)
	if err != nil {
		return fmt.Errorf("failed to get analysis for report %d: %w", report.Seq, err)
	}

	// Check if we have inferred contact emails
	if analysis.Classification == "digital" && analysis.InferredContactEmails != "" {
		// Split the comma-separated emails and send to each
		emails := strings.Split(strings.TrimSpace(analysis.InferredContactEmails), ",")
		var cleanEmails []string

		// Clean up each email (remove whitespace) and validate
		for _, email := range emails {
			cleanEmail := strings.TrimSpace(email)
			if cleanEmail != "" && s.isValidEmail(cleanEmail) {
				cleanEmails = append(cleanEmails, cleanEmail)
			} else if cleanEmail != "" {
				log.Warnf("Report %d: Invalid email address found in inferred contacts: %s", report.Seq, cleanEmail)
			}
		}

		if len(cleanEmails) > 0 {
			log.Infof("Report %d: Using %d valid inferred contact emails (priority over area emails): %v", report.Seq, len(cleanEmails), cleanEmails)

			// Send emails to inferred contacts (no area context needed)
			if err := s.sendEmailsToInferredContacts(ctx, report, analysis, cleanEmails); err != nil {
				log.Errorf("Failed to send emails to inferred contacts for report %d: %v", report.Seq, err)
			} else {
				log.Infof("Successfully sent emails to inferred contacts for report %d (%s report)", report.Seq, analysis.Classification)
			}

			// Mark report as processed and return
			return s.markReportAsProcessed(ctx, report.Seq)
		} else {
			log.Infof("Report %d: No valid inferred contact emails found after validation", report.Seq)
		}
	} else {
		log.Infof("Report %d: No inferred contact emails field found", report.Seq)
	}

	// Fall back to area-based email logic if no inferred emails
	log.Infof("Report %d: Falling back to area-based email logic (%s report)", report.Seq, analysis.Classification)

	// Find areas that contain this report point
	features, emails, err := s.findAreasForReport(ctx, report)
	if err != nil {
		return fmt.Errorf("failed to find areas for report: %w", err)
	}

	// If no areas found, mark as processed and return
	if len(emails) == 0 {
		log.Infof("Report %d: No areas found, marking as processed", report.Seq)
		return s.markReportAsProcessed(ctx, report.Seq)
	}

	log.Infof("Report %d: Found %d areas with emails, sending area-based emails", report.Seq, len(emails))

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
	ptWKT := fmt.Sprintf("POINT(%g %g)", report.Latitude, report.Longitude)

	// Find areas that contain this point
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx,
		"SELECT area_id FROM area_index WHERE MBRWithin(ST_GeomFromText(?, 4326), geom)",
		ptWKT)
	if err != nil {
		log.Errorf("area_index query error for report %d (in %s): %v", report.Seq, time.Since(qStart), err)
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

	log.Infof("Report %d: area_index returned %d area ids (in %s)", report.Seq, len(areaMap), time.Since(qStart))
	if len(areaMap) == 0 {
		log.Infof("Report %d: No areas found, marking as processed", report.Seq)
		return nil, nil, nil
	}

	log.Infof("Report %d: Found %d areas", report.Seq, len(areaMap))

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
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		log.Errorf("getAreaFeatures query error (in %s): %v", time.Since(qStart), err)
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

	log.Infof("getAreaFeatures fetched %d features (in %s)", len(features), time.Since(qStart))
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
	qStart := time.Now()
	rows, err := s.db.QueryContext(ctx, query, areaIDs...)
	if err != nil {
		log.Errorf("getAreaEmails query error (in %s): %v", time.Since(qStart), err)
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

	log.Infof("getAreaEmails fetched %d area->emails rows (in %s)", len(areaEmails), time.Since(qStart))
	return areaEmails, nil
}

// sendEmailsToInferredContacts sends emails to inferred contact emails without area context
func (s *EmailService) sendEmailsToInferredContacts(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, emails []string) error {
	if len(emails) == 0 {
		return nil
	}

	// Filter out opted-out emails
	var validEmails []string
	for _, email := range emails {
		optedOut, err := s.isEmailOptedOut(ctx, email)
		if err != nil {
			log.Warnf("Failed to check if email %s is opted out: %v, skipping", email, err)
			continue
		}

		if optedOut {
			log.Infof("Skipping opted-out email: %s", email)
			continue
		}

		validEmails = append(validEmails, email)
	}

	if len(validEmails) == 0 {
		log.Infof("All emails for report %d are opted out, no emails sent", report.Seq)
		return nil
	}

	log.Infof("Sending emails to %d valid inferred contacts for report %d (filtered from %d total)", len(validEmails), report.Seq, len(emails))

	// Generate map image for inferred contacts (1km map centered on report coordinates)
	mapImg, err := email.GeneratePolygonImg(nil, report.Latitude, report.Longitude)
	if err != nil {
		log.Warnf("Failed to generate map image for report %d: %v, sending email without map", report.Seq, err)
		// Continue without map image
	}

	// Send emails with analysis data and map image
	return s.email.SendEmailsWithAnalysis(validEmails, report.Image, mapImg, analysis)
}

// sendEmailsForArea sends emails for a specific area
func (s *EmailService) sendEmailsForArea(ctx context.Context, report models.Report, analysis *models.ReportAnalysis, feature *geojson.Feature, emails []string) error {
	if len(emails) == 0 {
		return nil
	}

	// Filter out opted-out emails
	var validEmails []string
	for _, email := range emails {
		optedOut, err := s.isEmailOptedOut(ctx, email)
		if err != nil {
			log.Warnf("Failed to check if email %s is opted out: %v, skipping", email, err)
			continue
		}

		if optedOut {
			log.Infof("Skipping opted-out email: %s", email)
			continue
		}

		validEmails = append(validEmails, email)
	}

	if len(validEmails) == 0 {
		log.Infof("All emails for area are opted out, no emails sent")
		return nil
	}

	log.Infof("Sending emails to %d valid contacts for area (filtered from %d total)", len(validEmails), len(emails))

	// Generate polygon image
	polyImg, err := email.GeneratePolygonImg(feature, report.Latitude, report.Longitude)
	if err != nil {
		return fmt.Errorf("failed to generate polygon image: %w", err)
	}

	// Send emails with analysis data
	return s.email.SendEmailsWithAnalysis(validEmails, report.Image, polyImg, analysis)
}

// getReportAnalysis gets the analysis data for a specific report
func (s *EmailService) getReportAnalysis(ctx context.Context, seq int64) (*models.ReportAnalysis, error) {
	qStart := time.Now()
	query := `
		SELECT seq, source, title, description, litter_probability, hazard_probability,
		severity_level, inferred_contact_emails, classification
		FROM report_analysis
		WHERE seq = ? AND language = 'en'
		LIMIT 1
	`

	var contact_emails sql.NullString
	var analysis models.ReportAnalysis
	err := s.db.QueryRowContext(ctx, query, seq).Scan(
		&analysis.Seq,
		&analysis.Source,
		&analysis.Title,
		&analysis.Description,
		&analysis.LitterProbability,
		&analysis.HazardProbability,
		&analysis.SeverityLevel,
		&contact_emails,
		&analysis.Classification,
	)
	if err != nil {
		log.Errorf("getReportAnalysis error for seq %d (in %s): %v", seq, time.Since(qStart), err)
		return nil, fmt.Errorf("failed to get analysis for seq %d: %w", seq, err)
	}

	analysis.InferredContactEmails = contact_emails.String

	log.Infof("getReportAnalysis loaded seq %d (in %s)", seq, time.Since(qStart))
	return &analysis, nil
}

// markReportAsProcessed marks a report as processed for email sending
func (s *EmailService) markReportAsProcessed(ctx context.Context, seq int64) error {
	start := time.Now()
	_, err := s.db.ExecContext(ctx, "INSERT INTO sent_reports_emails (seq) VALUES (?)", seq)
	if err != nil {
		log.Errorf("markReportAsProcessed error for seq %d (in %s): %v", seq, time.Since(start), err)
		return err
	}
	log.Infof("Report %d marked as processed (in %s)", seq, time.Since(start))
	return nil
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

	// Check if opted_out_emails table exists
	var optedOutTableExists int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = 'opted_out_emails'
	`).Scan(&optedOutTableExists)

	if err != nil {
		return fmt.Errorf("failed to check if opted_out_emails table exists: %w", err)
	}

	if optedOutTableExists == 0 {
		log.Info("Creating opted_out_emails table...")

		// Create the opted_out_emails table with proper indexing
		createOptedOutTableSQL := `
			CREATE TABLE opted_out_emails (
				id INT AUTO_INCREMENT PRIMARY KEY,
				email VARCHAR(255) NOT NULL UNIQUE,
				opted_out_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				INDEX idx_email (email),
				INDEX idx_opted_out_at (opted_out_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`

		_, err = db.ExecContext(ctx, createOptedOutTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create opted_out_emails table: %w", err)
		}

		log.Info("opted_out_emails table created successfully")
	} else {
		log.Info("opted_out_emails table already exists")
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

// AddOptedOutEmail adds an email to the opted_out_emails table
func (s *EmailService) AddOptedOutEmail(email string) error {
	ctx := context.Background()

	// Check if email already exists
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM opted_out_emails WHERE email = ?
	`, email).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check if email %s already exists: %w", email, err)
	}

	if count > 0 {
		return fmt.Errorf("email %s is already opted out", email)
	}

	// Insert new opted out email
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO opted_out_emails (email) VALUES (?)
	`, email)

	if err != nil {
		return fmt.Errorf("failed to add email %s to opted out list: %w", email, err)
	}

	log.Infof("Email %s has been opted out successfully", email)
	return nil
}
