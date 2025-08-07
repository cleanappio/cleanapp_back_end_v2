package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
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

// GetReportsSince retrieves reports with analysis since a given sequence number
// Only returns reports that are not resolved (either no status or status = 'active')
func (d *Database) GetReportsSince(ctx context.Context, sinceSeq int) ([]models.ReportWithAnalysis, error) {
	// First, get all reports since the given sequence that are not resolved
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.seq > ? 
		AND (rs.status IS NULL OR rs.status = 'active') AND (ra.hazard_probability >= 0.5 OR ra.litter_probability >= 0.5)
		AND ra.is_valid = TRUE
		ORDER BY r.seq ASC
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
	var reportSeqs []int
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Latitude,
			&report.Longitude,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq ASC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysis.AnalysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}
		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
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
func (d *Database) GetReportCount(ctx context.Context, classification string) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports r JOIN report_analysis ra ON r.seq = ra.seq WHERE ra.classification = ?", classification).Scan(&count)
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

// GetLastNAnalyzedReports retrieves the last N analyzed reports
// If full_data is true, returns reports with analysis. If false, returns only reports.
func (d *Database) GetLastNAnalyzedReports(ctx context.Context, limit int, classification string, full_data bool) (interface{}, error) {
	// First, get the last N reports that have analysis and are not resolved
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE (rs.status IS NULL OR rs.status = 'active') AND (ra.hazard_probability >= 0.5 OR ra.litter_probability >= 0.5) AND ra.classification = ?
		AND ra.is_valid = TRUE
		ORDER BY r.seq DESC
		LIMIT ?
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, classification, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query last N analyzed reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
	var reportSeqs []int
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Latitude,
			&report.Longitude,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating last N analyzed reports: %w", err)
	}

	if len(reports) == 0 {
		if full_data {
			return []models.ReportWithAnalysis{}, nil
		}
		return []models.Report{}, nil
	}

	// If full_data is false, return only reports
	if !full_data {
		return reports, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysis.AnalysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}
		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
}

// GetReportBySeq retrieves a single report with analysis by sequence ID
// Only returns reports that are not resolved (either no status or status = 'active')
func (d *Database) GetReportBySeq(ctx context.Context, seq int) (*models.ReportWithAnalysis, error) {
	// First, get the report if it's not resolved
	reportQuery := `
		SELECT r.seq, r.ts, r.id, r.latitude, r.longitude, r.image
		FROM reports r
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.seq = ? AND (rs.status IS NULL OR rs.status = 'active')
	`

	var report models.Report
	err := d.db.QueryRowContext(ctx, reportQuery, seq).Scan(
		&report.Seq,
		&report.Timestamp,
		&report.ID,
		&report.Latitude,
		&report.Longitude,
		&report.Image,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found or is resolved", seq)
		}
		return nil, fmt.Errorf("failed to get report by seq: %w", err)
	}

	// Then, get all analyses for this report
	analysesQuery := `
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq = ?
		ORDER BY ra.language ASC
	`

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, seq)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	var analyses []models.ReportAnalysis
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysis.AnalysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}
		analyses = append(analyses, analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	if len(analyses) == 0 {
		return nil, fmt.Errorf("no analyses found for report with seq %d", seq)
	}

	return &models.ReportWithAnalysis{
		Report:   report,
		Analysis: analyses,
	}, nil
}

// GetLastNReportsByID retrieves the last N reports with analysis for a given report ID
func (d *Database) GetLastNReportsByID(ctx context.Context, reportID string, classification string, limit int) ([]models.ReportWithAnalysis, error) {
	// First, get the last N reports for the given ID that are not resolved
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude, r.image
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.id = ? AND (rs.status IS NULL OR rs.status = 'active') AND ra.is_valid = TRUE
		AND (ra.hazard_probability >= 0.5 OR ra.litter_probability >= 0.5) AND ra.classification = ?
		ORDER BY r.seq DESC
		LIMIT ?
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, reportID, classification, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports by ID: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
	var reportSeqs []int
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Latitude,
			&report.Longitude,
			&report.Image,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports by ID: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysis.AnalysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}
		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
}

// GetReportsByLatLng retrieves reports within a bounding box around given coordinates
// Only returns reports that are not resolved (either no status or status = 'active')
func (d *Database) GetReportsByLatLng(ctx context.Context, latitude, longitude float64, radiusKm float64, n int) ([]models.ReportWithAnalysis, error) {
	// Calculate bounding box coordinates
	// Convert radius from km to degrees (approximate: 1 degree ≈ 111 km)
	radiusDegrees := radiusKm / 111.0

	minLat := latitude - radiusDegrees
	maxLat := latitude + radiusDegrees
	minLng := longitude - radiusDegrees
	maxLng := longitude + radiusDegrees

	// First, get all reports within the bounding box that are not resolved
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude, r.image
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		WHERE r.latitude BETWEEN ? AND ?
		AND r.longitude BETWEEN ? AND ?
		AND (rs.status IS NULL OR rs.status = 'active')
		AND (ra.hazard_probability >= 0.5 OR ra.litter_probability >= 0.5)
		AND ra.is_valid = TRUE
		AND ra.classification = 'physical'
		ORDER BY r.ts DESC
		LIMIT ?
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, minLat, maxLat, minLng, maxLng, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports by lat/lng: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences and reports
	var reportSeqs []int
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Latitude,
			&report.Longitude,
			&report.Image,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
		reportSeqs = append(reportSeqs, report.Seq)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Then, get all analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, 
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)
	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysis.AnalysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}
		analysesBySeq[analysis.Seq] = append(analysesBySeq[analysis.Seq], analysis)
	}

	if err = analysisRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analyses: %w", err)
	}

	// Combine reports with their analyses
	var result []models.ReportWithAnalysis
	for _, report := range reports {
		analyses := analysesBySeq[report.Seq]
		if len(analyses) == 0 {
			// Skip reports without analyses
			continue
		}

		result = append(result, models.ReportWithAnalysis{
			Report:   report,
			Analysis: analyses,
		})
	}

	return result, nil
}
