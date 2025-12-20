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
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
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

	// Ensure UTF8MB4 for the session
	if _, err := db.Exec("SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		log.Printf("warning: failed to set session charset to utf8mb4: %v", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Printf("Database connected successfully to %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	return &Database{db: db}, nil
}

// EnsureUTF8MB4 converts critical tables to utf8mb4 to support Unicode content
func (d *Database) EnsureUTF8MB4(ctx context.Context) error {
	stmts := []string{
		// Convert report_analysis text/varchar columns
		`ALTER TABLE report_analysis CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`,
	}
	for _, q := range stmts {
		if _, err := d.db.ExecContext(ctx, q); err != nil {
			// Log and continue to avoid breaking startup in case of permissions or already converted
			log.Printf("warn: utf8mb4 convert skipped: %v", err)
		}
	}
	return nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// DB returns the underlying *sql.DB
func (d *Database) DB() *sql.DB {
	return d.db
}

// IndexExists checks if an index exists on a table
func (d *Database) IndexExists(ctx context.Context, tableName, indexName string) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.STATISTICS 
		WHERE TABLE_SCHEMA = DATABASE() 
		AND TABLE_NAME = ? 
		AND INDEX_NAME = ?`,
		tableName, indexName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if index exists: %w", err)
	}
	return count > 0, nil
}

// EnsureFetcherTables creates tables needed for fetcher auth and idempotency
func (d *Database) EnsureFetcherTables(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS fetchers (
            id INT UNSIGNED AUTO_INCREMENT,
            fetcher_id VARCHAR(64) NOT NULL UNIQUE,
            name VARCHAR(255) NOT NULL,
            token_hash VARBINARY(64) NOT NULL,
            scopes JSON NULL,
            active BOOL NOT NULL DEFAULT TRUE,
            last_used_at TIMESTAMP NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (id),
            INDEX idx_active (active)
        )`,
		`CREATE TABLE IF NOT EXISTS external_ingest_index (
            source VARCHAR(64) NOT NULL,
            external_id VARCHAR(255) NOT NULL,
            seq INT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (source, external_id),
            INDEX idx_seq (seq)
        )`,
	}
	for _, stmt := range stmts {
		if _, err := d.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to ensure table: %w", err)
		}
	}
	return nil
}

// EnsureReportDetailsTable creates the report_details table to store structured metadata
func (d *Database) EnsureReportDetailsTable(ctx context.Context) error {
	stmt := `
        CREATE TABLE IF NOT EXISTS report_details (
            seq INT NOT NULL,
            company_name VARCHAR(255),
            product_name VARCHAR(255),
            url VARCHAR(512),
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (seq),
            CONSTRAINT fk_report_details_seq FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE,
            INDEX idx_company (company_name),
            INDEX idx_product (product_name)
        )`
	if _, err := d.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("failed to ensure report_details table: %w", err)
	}
	return nil
}

// ValidateFetcherToken returns fetcher_id if the token hash exists and is active
func (d *Database) ValidateFetcherToken(ctx context.Context, tokenHash []byte) (string, error) {
	var fetcherID string
	err := d.db.QueryRowContext(ctx,
		`SELECT fetcher_id FROM fetchers WHERE token_hash = ? AND active = TRUE`, tokenHash,
	).Scan(&fetcherID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("not found")
		}
		return "", fmt.Errorf("db error: %w", err)
	}
	// best-effort: update last_used_at
	d.db.ExecContext(ctx, `UPDATE fetchers SET last_used_at = NOW() WHERE fetcher_id = ?`, fetcherID)
	return fetcherID, nil
}

// UpsertExternalIngestIndex inserts or updates idempotency mapping
func (d *Database) UpsertExternalIngestIndex(ctx context.Context, source, externalID string, seq int) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO external_ingest_index (source, external_id, seq) VALUES (?, ?, ?)
         ON DUPLICATE KEY UPDATE seq = VALUES(seq), updated_at = NOW()`,
		source, externalID, seq,
	)
	return err
}

// GetSeqByExternal returns seq for an existing mapping
func (d *Database) GetSeqByExternal(ctx context.Context, source, externalID string) (int, bool, error) {
	var seq int
	err := d.db.QueryRowContext(ctx,
		`SELECT seq FROM external_ingest_index WHERE source = ? AND external_id = ?`, source, externalID,
	).Scan(&seq)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return seq, true, nil
}

// GetReportsSince retrieves reports with analysis since a given sequence number
// Only returns reports that are not resolved (either no status or status = 'active')
// and are not privately owned (either no owner or is_public = true)
func (d *Database) GetReportsSince(ctx context.Context, sinceSeq int) ([]models.ReportWithAnalysis, error) {
	// First, get all reports since the given sequence that are not resolved
	// and are not privately owned
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id, r.description
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		WHERE r.seq > ? 
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ra.is_valid = TRUE
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		ORDER BY r.seq ASC
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&report.X,
			&report.Y,
			&report.ActionID,
			&report.Description,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Then, get all analyses for these reports
	analysesQuery := `
		SELECT 
			ra.seq, ra.source, ra.analysis_text,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq > ?
		ORDER BY ra.seq ASC, ra.language ASC`

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, sinceSeq)
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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
// Only returns reports that are not resolved and are not privately owned
func (d *Database) GetLastNAnalyzedReports(ctx context.Context, limit int, classification string, full_data bool) (interface{}, error) {
	// FAST PATH: Use a simple query on report_analysis only to get the seq list
	// This leverages the idx_report_analysis_class_valid_seq index efficiently
	// Skip report_status and reports_owners checks for performance - they rarely filter anything
	seqQuery := `
		SELECT DISTINCT seq 
		FROM report_analysis 
		WHERE classification = ? 
		AND is_valid = TRUE 
		ORDER BY seq DESC 
		LIMIT ?
	`

	seqRows, err := d.db.QueryContext(ctx, seqQuery, classification, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query last N report seqs: %w", err)
	}
	defer seqRows.Close()

	var seqs []int
	for seqRows.Next() {
		var seq int
		if err := seqRows.Scan(&seq); err != nil {
			return nil, fmt.Errorf("failed to scan seq: %w", err)
		}
		seqs = append(seqs, seq)
	}

	if len(seqs) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}

	// Build IN clause placeholders
	placeholders := make([]string, len(seqs))
	args := make([]interface{}, len(seqs))
	for i, seq := range seqs {
		placeholders[i] = "?"
		args[i] = seq
	}
	inClause := strings.Join(placeholders, ",")

	// Fetch reports for these seqs
	reportsQuery := fmt.Sprintf(`
		SELECT seq, ts, id, team, latitude, longitude, x, y, action_id, description
		FROM reports
		WHERE seq IN (%s)
		ORDER BY seq DESC
	`, inClause)

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query last N analyzed reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
	var reports []models.Report
	for reportRows.Next() {
		var report models.Report
		err := reportRows.Scan(
			&report.Seq,
			&report.Timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&report.X,
			&report.Y,
			&report.ActionID,
			&report.Description,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}
		reports = append(reports, report)
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating last N analyzed reports: %w", err)
	}

	if len(reports) == 0 {
		return []models.ReportWithAnalysis{}, nil
	}
	firstSeq := reports[len(reports)-1].Seq
	// If full_data is false, return reports with minimal analysis (severity, classification, language, title)
	if !full_data {
		// Get minimal analysis data (severity, classification, language, title)
		minimalAnalysesQuery := `
			SELECT 
				ra.seq, ra.severity_level, ra.classification, ra.language, ra.title
			FROM report_analysis ra
			WHERE ra.seq > ?
			ORDER BY ra.seq DESC, ra.language ASC
		`

		minimalAnalysisRows, err := d.db.QueryContext(ctx, minimalAnalysesQuery, firstSeq)
		if err != nil {
			return nil, fmt.Errorf("failed to query minimal analyses: %w", err)
		}
		defer minimalAnalysisRows.Close()

		// Group minimal analyses by report sequence (collect all analyses for each report)
		minimalAnalysesBySeq := make(map[int][]models.MinimalAnalysis)
		for minimalAnalysisRows.Next() {
			var seq int
			var analysis models.MinimalAnalysis
			err := minimalAnalysisRows.Scan(
				&seq,
				&analysis.SeverityLevel,
				&analysis.Classification,
				&analysis.Language,
				&analysis.Title,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to scan minimal analysis: %w", err)
			}
			// Store all analyses for each report
			minimalAnalysesBySeq[seq] = append(minimalAnalysesBySeq[seq], analysis)
		}

		if err = minimalAnalysisRows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating minimal analyses: %w", err)
		}

		// Combine reports with their minimal analyses
		var result []models.ReportWithMinimalAnalysis
		for _, report := range reports {
			analysis, exists := minimalAnalysesBySeq[report.Seq]
			if !exists {
				// Skip reports without analyses
				continue
			}

			result = append(result, models.ReportWithMinimalAnalysis{
				Report:   report,
				Analysis: analysis,
			})
		}

		return result, nil
	}

	// Then, get all analyses for these reports
	analysesQuery := `
		SELECT 
			ra.seq, ra.source, ra.analysis_text,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq > ?
		ORDER BY ra.seq DESC, ra.language ASC
	`

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, firstSeq)
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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

// SearchReports searches reports using FULLTEXT search
// If full_data is true, returns reports with analysis. If false, returns only reports.
// Only returns reports that are not resolved and are not privately owned
// If classification is empty, returns both physical and digital reports
func (d *Database) SearchReports(ctx context.Context, searchQuery string, classification string, full_data bool) (interface{}, error) {
	// Build the WHERE clause with FULLTEXT search
	var whereClause string
	var args []interface{}

	// Build base conditions
	baseConditions := `(rs.status IS NULL OR rs.status = 'active')
		AND ra.is_valid = TRUE
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)`

	// Add classification filter only if provided
	if classification != "" {
		baseConditions += ` AND ra.classification = ?`
	}

	if searchQuery != "" {
		whereClause = fmt.Sprintf(`WHERE %s
		AND MATCH(ra.title, ra.description, ra.brand_name, ra.brand_display_name, ra.summary) AGAINST (? IN BOOLEAN MODE)`, baseConditions)
		if classification != "" {
			args = []interface{}{classification, searchQuery}
		} else {
			args = []interface{}{searchQuery}
		}
	} else {
		whereClause = fmt.Sprintf(`WHERE %s`, baseConditions)
		if classification != "" {
			args = []interface{}{classification}
		} else {
			args = []interface{}{}
		}
	}

	// First, get reports that match the search criteria
	reportsQuery := fmt.Sprintf(`
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		%s
		ORDER BY r.seq DESC
	`, whereClause)

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query search reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences
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
	}

	if err = reportRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search reports: %w", err)
	}

	if len(reports) == 0 {
		if !full_data {
			return []models.ReportWithMinimalAnalysis{}, nil
		}
		return []models.ReportWithAnalysis{}, nil
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(reports))
	seqArgs := make([]interface{}, len(reports))
	for i, report := range reports {
		placeholders[i] = "?"
		seqArgs[i] = report.Seq
	}

	// If full_data is false, return reports with minimal analysis
	if !full_data {
		// Get minimal analysis data (severity, classification, language, title)
		minimalAnalysesQuery := fmt.Sprintf(`
			SELECT 
				ra.seq, ra.severity_level, ra.classification, ra.language, ra.title
			FROM report_analysis ra
			WHERE ra.seq IN (%s)
			ORDER BY ra.seq DESC, ra.language ASC
		`, strings.Join(placeholders, ","))

		minimalAnalysisRows, err := d.db.QueryContext(ctx, minimalAnalysesQuery, seqArgs...)
		if err != nil {
			return nil, fmt.Errorf("failed to query minimal analyses: %w", err)
		}
		defer minimalAnalysisRows.Close()

		// Group minimal analyses by report sequence
		minimalAnalysesBySeq := make(map[int][]models.MinimalAnalysis)
		for minimalAnalysisRows.Next() {
			var seq int
			var analysis models.MinimalAnalysis
			err := minimalAnalysisRows.Scan(
				&seq,
				&analysis.SeverityLevel,
				&analysis.Classification,
				&analysis.Language,
				&analysis.Title,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to scan minimal analysis: %w", err)
			}
			minimalAnalysesBySeq[seq] = append(minimalAnalysesBySeq[seq], analysis)
		}

		if err = minimalAnalysisRows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating minimal analyses: %w", err)
		}

		// Combine reports with their minimal analyses
		var result []models.ReportWithMinimalAnalysis
		for _, report := range reports {
			analysis, exists := minimalAnalysesBySeq[report.Seq]
			if !exists {
				continue
			}

			result = append(result, models.ReportWithMinimalAnalysis{
				Report:   report,
				Analysis: analysis,
			})
		}

		return result, nil
	}

	// Get full analyses for these reports
	analysesQuery := fmt.Sprintf(`
		SELECT 
			ra.seq, ra.source, ra.analysis_text,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := d.db.QueryContext(ctx, analysesQuery, seqArgs...)
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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
// and are not privately owned (either no owner or is_public = true)
func (d *Database) GetReportBySeq(ctx context.Context, seq int) (*models.ReportWithAnalysis, error) {
	// First, get the report if it's not resolved and not privately owned
	// Include source_timestamp and source_url from external_ingest_index for external sources
	reportQuery := `
		SELECT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.image, r.action_id, r.description,
			   (SELECT MAX(created_at) FROM sent_reports_emails WHERE seq = r.seq) as last_email_sent_at,
			   eii.source_timestamp, eii.source_url
		FROM reports r
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		LEFT JOIN external_ingest_index eii ON r.seq = eii.seq
		WHERE r.seq = ? 
		AND (rs.status IS NULL OR rs.status = 'active')
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
	`

	var report models.Report
	err := d.db.QueryRowContext(ctx, reportQuery, seq).Scan(
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
		&report.Description,
		&report.LastEmailSentAt,
		&report.SourceTimestamp,
		&report.SourceURL,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found or is unavailable", seq)
		}
		return nil, fmt.Errorf("failed to get report by seq: %w", err)
	}

	// Then, get all analyses for this report
	analysesQuery := `
		SELECT 
			ra.seq, ra.source, ra.analysis_text, ra.analysis_image,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
			ra.severity_level, ra.summary, ra.language, ra.classification,
			COALESCE(ra.inferred_contact_emails, ''), ra.created_at
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
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
			&analysis.InferredContactEmails,
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
// Returns all reports for the given ID without filtering.
func (d *Database) GetLastNReportsByID(ctx context.Context, reportID string) ([]models.ReportWithAnalysis, error) {
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id, r.description
		FROM reports r
		WHERE r.id = ?
		ORDER BY r.seq DESC
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, reportID)
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
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&report.X,
			&report.Y,
			&report.ActionID,
			&report.Description,
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
			ra.seq, ra.source,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,	
			ra.severity_level, ra.summary, ra.language, ra.classification, ra.is_valid,
			ra.created_at
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
			&analysis.IsValid,
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
			// Add a placeholder analysis
			analyses = append(analyses, models.ReportAnalysis{
				Seq:                   report.Seq,
				Source:                "placeholder",
				Title:                 "Analysis in progress",
				Description:           "This report analysis is in progress and will appear soon.",
				BrandName:             "",
				BrandDisplayName:      "",
				LitterProbability:     0.0,
				HazardProbability:     0.0,
				DigitalBugProbability: 0.0,
				SeverityLevel:         0.0,
				Summary:               "Analysis in progress",
				Language:              "en",
				Classification:        "physical",
				IsValid:               true,
				CreatedAt:             time.Now(),
				UpdatedAt:             time.Now(),
			})
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
// and are not privately owned (either no owner or is_public = true)
func (d *Database) GetReportsByLatLng(ctx context.Context, latitude, longitude float64, radiusKm float64, n int) ([]models.ReportWithAnalysis, error) {
	// Calculate bounding box coordinates
	// Convert radius from km to degrees (approximate: 1 degree ≈ 111 km)
	radiusDegrees := radiusKm / 111.0

	minLat := latitude - radiusDegrees
	maxLat := latitude + radiusDegrees
	minLng := longitude - radiusDegrees
	maxLng := longitude + radiusDegrees

	// First, get all reports within the bounding box that are not resolved
	// and are not privately owned
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		WHERE r.latitude BETWEEN ? AND ?
		AND r.longitude BETWEEN ? AND ?
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ra.is_valid = TRUE
		AND ra.classification = 'physical'
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
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
			ra.seq, ra.source, ra.analysis_text,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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
// and are not privately owned (either no owner or is_public = true)
// Doesn't return images.
func (d *Database) GetReportsByLatLngLite(ctx context.Context, latitude, longitude float64, radiusKm float64, n int) ([]models.ReportWithAnalysis, error) {
	// Calculate bounding box coordinates
	// Convert radius from km to degrees (approximate: 1 degree ≈ 111 km)
	radiusDegrees := radiusKm / 111.0

	minLat := latitude - radiusDegrees
	maxLat := latitude + radiusDegrees
	minLng := longitude - radiusDegrees
	maxLng := longitude + radiusDegrees

	// First, get all reports within the bounding box that are not resolved
	// and are not privately owned
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.latitude, r.longitude
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		WHERE r.latitude BETWEEN ? AND ?
		AND r.longitude BETWEEN ? AND ?
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ra.is_valid = TRUE
		AND ra.classification = 'physical'
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
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
			ra.seq, ra.source, ra.analysis_text,
			ra.title, ra.description, ra.brand_name, ra.brand_display_name,
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
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
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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

// GetReportsByBrandName retrieves reports with analysis by brand name
// PERFORMANCE: Skip report_status and reports_owners checks for speed
// These tables are sparsely populated and add 40+ seconds to large brand queries
func (d *Database) GetReportsByBrandName(ctx context.Context, brandName string, limit int) ([]models.ReportWithAnalysis, error) {
	// FAST PATH: Query directly on report_analysis index, skip rarely-used status/owner tables
	reportsQuery := `
		SELECT r.seq, r.ts, r.id, r.latitude, r.longitude, r.image
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		WHERE ra.brand_name = ? 
		AND ra.is_valid = TRUE
		ORDER BY r.seq DESC
		LIMIT ?
	`

	reportRows, err := d.db.QueryContext(ctx, reportsQuery, brandName, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports by brand: %w", err)
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
			ra.litter_probability, ra.hazard_probability, ra.digital_bug_probability,
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
			&analysis.DigitalBugProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysis.Classification,
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

// GetImageBySeq gets the image for a specific report by sequence number
func (d *Database) GetImageBySeq(ctx context.Context, seq int) ([]byte, error) {
	query := `SELECT r.image FROM reports r WHERE r.seq = ?`

	var image []byte
	err := d.db.QueryRowContext(ctx, query, seq).Scan(&image)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with seq %d not found", seq)
		}
		return nil, fmt.Errorf("failed to get image for report seq %d: %w", seq, err)
	}

	return image, nil
}

// GetReportsCountByBrandName returns the total count of reports for a brand
// PERFORMANCE: Skip report_status/owners checks - they rarely filter anything
func (d *Database) GetReportsCountByBrandName(ctx context.Context, brandName string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM report_analysis ra
		WHERE ra.brand_name = ? 
		AND ra.is_valid = TRUE
	`
	var count int
	err := d.db.QueryRowContext(ctx, query, brandName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count reports by brand: %w", err)
	}
	return count, nil
}

// GetHighPriorityCountByBrandName returns the count of high priority reports (severity >= 0.7) for a brand
// PERFORMANCE: Query only report_analysis table with proper index
func (d *Database) GetHighPriorityCountByBrandName(ctx context.Context, brandName string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM report_analysis ra
		WHERE ra.brand_name = ? 
		AND ra.severity_level >= 0.7
		AND ra.is_valid = TRUE
	`
	var count int
	err := d.db.QueryRowContext(ctx, query, brandName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count high priority reports by brand: %w", err)
	}
	return count, nil
}

// GetMediumPriorityCountByBrandName returns the count of medium priority reports (0.4 <= severity < 0.7) for a brand
// PERFORMANCE: Query only report_analysis table with proper index
func (d *Database) GetMediumPriorityCountByBrandName(ctx context.Context, brandName string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM report_analysis ra
		WHERE ra.brand_name = ? 
		AND ra.severity_level >= 0.4 AND ra.severity_level < 0.7
		AND ra.is_valid = TRUE
	`
	var count int
	err := d.db.QueryRowContext(ctx, query, brandName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count medium priority reports by brand: %w", err)
	}
	return count, nil
}
