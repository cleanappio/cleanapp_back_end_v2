package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"brand-dashboard/config"
	"brand-dashboard/models"
	"brand-dashboard/utils"

	_ "github.com/go-sql-driver/mysql"
)

// DatabaseService manages database connections and queries for brand-related reports
type DatabaseService struct {
	db  *sql.DB
	Cfg *config.Config
}

// NewDatabaseService creates a new database service
func NewDatabaseService(cfg *config.Config) (*DatabaseService, error) {
	// Create DSN using config parameters
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)

	// Open database connection
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

	log.Printf("Database connection established to %s:%s/%s", cfg.DBHost, cfg.DBPort, cfg.DBName)

	return &DatabaseService{db: db, Cfg: cfg}, nil
}

// Close closes the database connection
func (s *DatabaseService) Close() error {
	return s.db.Close()
}

// GetReportsByBrand gets the last n reports with analysis that match a specific brand
// Only returns reports that are not privately owned by other users
func (s *DatabaseService) GetReportsByBrand(brandName string, n int, userID string) ([]models.ReportWithAnalysis, error) {
	// Normalize the brand name for exact matching
	normalizedBrandName := utils.NormalizeBrandName(brandName)

	// Get reports with analyses that match the brand name exactly
	// Filter out reports that are privately owned by other users
	reportsQuery := `
		SELECT DISTINCT r.seq, r.ts, r.id, r.team, r.latitude, r.longitude, r.x, r.y, r.action_id
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		WHERE ra.brand_name = ?
		AND ra.classification = 'physical'
		AND (rs.status IS NULL OR rs.status = 'active')
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE OR ro.owner = ?)
		ORDER BY r.ts DESC
		LIMIT ?
	`

	reportRows, err := s.db.Query(reportsQuery, normalizedBrandName, userID, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer reportRows.Close()

	// Collect all report sequences and reports
	var reportSeqs []int
	var reports []models.ReportData
	for reportRows.Next() {
		var report models.ReportData
		var timestamp time.Time
		var x, y sql.NullFloat64
		var actionID sql.NullString

		err := reportRows.Scan(
			&report.Seq,
			&timestamp,
			&report.ID,
			&report.Team,
			&report.Latitude,
			&report.Longitude,
			&x,
			&y,
			&actionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan report: %w", err)
		}

		// Convert timestamp to string
		report.Timestamp = timestamp.Format(time.RFC3339)

		// Handle nullable fields
		if x.Valid {
			report.X = &x.Float64
		}
		if y.Valid {
			report.Y = &y.Float64
		}
		if actionID.Valid {
			report.ActionID = &actionID.String
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
			ra.severity_level, ra.summary, ra.language, ra.created_at
		FROM report_analysis ra
		WHERE ra.seq IN (%s)
		ORDER BY ra.seq DESC, ra.language ASC
	`, strings.Join(placeholders, ","))

	analysisRows, err := s.db.Query(analysesQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query analyses: %w", err)
	}
	defer analysisRows.Close()

	// Group analyses by report sequence
	analysesBySeq := make(map[int][]models.ReportAnalysis)

	for analysisRows.Next() {
		var analysis models.ReportAnalysis
		var analysisCreatedAt time.Time
		var analysisImage sql.NullString

		err := analysisRows.Scan(
			&analysis.Seq,
			&analysis.Source,
			&analysis.AnalysisText,
			&analysisImage,
			&analysis.Title,
			&analysis.Description,
			&analysis.BrandName,
			&analysis.BrandDisplayName,
			&analysis.LitterProbability,
			&analysis.HazardProbability,
			&analysis.SeverityLevel,
			&analysis.Summary,
			&analysis.Language,
			&analysisCreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analysis: %w", err)
		}

		// Convert timestamp to string
		analysis.CreatedAt = analysisCreatedAt.Format(time.RFC3339)

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

// GetBrandsInfo returns information about all configured brands with their report counts
func (s *DatabaseService) GetBrandsInfo(userID string) ([]models.BrandInfo, error) {
	var brandsInfo []models.BrandInfo

	for _, brandName := range s.Cfg.BrandNames {
		// Count reports for this brand
		count, err := s.getBrandReportCount(brandName, userID)
		if err != nil {
			log.Printf("Warning: failed to get report count for brand %s: %v", brandName, err)
			count = 0
		}

		brandsInfo = append(brandsInfo, models.BrandInfo{
			Name:        utils.NormalizeBrandName(brandName),
			DisplayName: brandName,
			Count:       count,
		})
	}

	return brandsInfo, nil
}

// getBrandReportCount gets the count of reports for a specific brand
// Only counts reports that are not privately owned by other users
func (s *DatabaseService) getBrandReportCount(brandName string, userID string) (int, error) {
	query := `
		SELECT COUNT(DISTINCT r.seq)
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		WHERE ra.brand_name IS NOT NULL AND ra.brand_name != ''
		AND ra.classification = 'physical'
		AND (rs.status IS NULL OR rs.status = 'active')
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE OR ro.owner = ?)
	`

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to query brand report count: %w", err)
	}
	defer rows.Close()

	var totalCount int
	for rows.Next() {
		var count int
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("failed to scan brand report count: %w", err)
		}

		// Check if this analysis matches the target brand
		if isMatch, _ := s.Cfg.IsBrandMatch(brandName); isMatch {
			totalCount += count
		}
	}

	return totalCount, nil
}
