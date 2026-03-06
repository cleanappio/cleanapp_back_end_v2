package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"report-ownership-service/models"
)

// OwnershipService handles report ownership determination
type OwnershipService struct {
	db *sql.DB
}

// NewOwnershipService creates a new ownership service instance
func NewOwnershipService(db *sql.DB) *OwnershipService {
	return &OwnershipService{
		db: db,
	}
}

// DetermineLocationOwners determines owners based on report location
func (s *OwnershipService) DetermineLocationOwners(ctx context.Context, latitude, longitude float64) ([]models.OwnerWithPublicFlag, error) {
	query := `
		SELECT DISTINCT ca.customer_id, ca.is_public
		FROM customer_areas ca
		JOIN areas a ON ca.area_id = a.id
		JOIN area_index ai ON a.id = ai.area_id
		WHERE ST_Contains(ai.geom, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
	`

	rows, err := s.db.QueryContext(ctx, query, latitude, longitude)
	if err != nil {
		return nil, fmt.Errorf("failed to query location ownership: %w", err)
	}
	defer rows.Close()

	var owners []models.OwnerWithPublicFlag
	for rows.Next() {
		var owner models.OwnerWithPublicFlag
		if err := rows.Scan(&owner.CustomerID, &owner.IsPublic); err != nil {
			log.Printf("ERROR: Failed to scan customer ID and is_public: %v", err)
			continue
		}
		owners = append(owners, owner)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating location ownership: %w", err)
	}

	return owners, nil
}

// DetermineBrandOwners determines owners based on report brand
func (s *OwnershipService) DetermineBrandOwners(ctx context.Context, brandName string) ([]models.OwnerWithPublicFlag, error) {
	if brandName == "" {
		return []models.OwnerWithPublicFlag{}, nil
	}

	normalizedBrand := normalizeBrandName(brandName)
	query := `
		SELECT DISTINCT customer_id, is_public
		FROM customer_brands
		WHERE brand_name = ?
	`

	rows, err := s.db.QueryContext(ctx, query, normalizedBrand)
	if err != nil {
		return nil, fmt.Errorf("failed to query brand ownership: %w", err)
	}
	defer rows.Close()

	var owners []models.OwnerWithPublicFlag
	for rows.Next() {
		var owner models.OwnerWithPublicFlag
		if err := rows.Scan(&owner.CustomerID, &owner.IsPublic); err != nil {
			log.Printf("ERROR: Failed to scan customer ID and is_public: %v", err)
			continue
		}
		owners = append(owners, owner)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating brand ownership: %w", err)
	}

	log.Printf("DEBUG: Brand: %s, Brand owners: %+v", brandName, owners)

	return owners, nil
}

// StoreReportOwners stores ownership information for a report
func (s *OwnershipService) StoreReportOwners(ctx context.Context, seq int, owners []string, public_flags []bool) error {
	// Always store at least one record, even if no owners
	if len(owners) == 0 {
		// Store a record with empty owner and is_public = TRUE to mark this report as processed and public
		query := `INSERT IGNORE INTO reports_owners (seq, owner, is_public) VALUES (?, '', FALSE)`
		_, err := s.db.ExecContext(ctx, query, seq)
		if err != nil {
			return fmt.Errorf("failed to store report with no owners: %w", err)
		}
		return nil
	}

	// Build the INSERT statement for reports with owners
	placeholders := make([]string, len(owners))
	args := make([]any, len(owners)*3) // seq, owner, is_public

	for i, owner := range owners {
		placeholders[i] = "(?, ?, ?)" // is_public = FALSE for reports with owners
		args[i*3] = seq
		args[i*3+1] = owner
		args[i*3+2] = public_flags[i]
		// is_public is set to FALSE in the placeholder
	}

	query := fmt.Sprintf(`
		INSERT IGNORE INTO reports_owners (seq, owner, is_public)
		VALUES %s
	`, strings.Join(placeholders, ","))

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to store report owners: %w", err)
	}

	return nil
}

// GetLastProcessedSeq gets the last processed sequence number
func (s *OwnershipService) GetLastProcessedSeq(ctx context.Context) (int, error) {
	var seq int
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) FROM reports_owners").Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("failed to get last processed seq: %w", err)
	}
	return seq, nil
}

// GetTotalReports gets the total number of reports
func (s *OwnershipService) GetTotalReports(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total reports: %w", err)
	}
	return count, nil
}

// GetTotalProcessedReports gets the total number of processed reports
func (s *OwnershipService) GetTotalProcessedReports(ctx context.Context) (int, error) {
	var count int
	// Count unique reports that have at least one owner row.
	// Avoid COUNT(DISTINCT ...) by grouping first.
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT seq FROM reports_owners GROUP BY seq
		) grouped
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total processed reports: %w", err)
	}
	return count, nil
}

// GetPublicReports gets reports that are public (have no owners)
func (s *OwnershipService) GetPublicReports(ctx context.Context) ([]int, error) {
	query := `SELECT DISTINCT seq FROM reports_owners WHERE is_public = TRUE`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query public reports: %w", err)
	}
	defer rows.Close()

	var seqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			log.Printf("ERROR: Failed to scan seq: %v", err)
			continue
		}
		seqs = append(seqs, seq)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating public reports: %w", err)
	}

	return seqs, nil
}

// GetReportWithAnalysis loads the canonical report and all persisted analysis rows for a seq.
func (s *OwnershipService) GetReportWithAnalysis(ctx context.Context, seq int) (*models.ReportWithAnalysis, error) {
	var report models.Report
	err := s.db.QueryRowContext(ctx, `
		SELECT seq, ts, id, team, latitude, longitude, x, y, image, action_id, description
		FROM reports
		WHERE seq = ?
	`, seq).Scan(
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
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report seq=%d not found", seq)
		}
		return nil, fmt.Errorf("load report seq=%d: %w", seq, err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			seq,
			COALESCE(source, '') AS source,
			COALESCE(analysis_text, '') AS analysis_text,
			analysis_image,
			COALESCE(title, '') AS title,
			COALESCE(description, '') AS description,
			COALESCE(brand_name, '') AS brand_name,
			COALESCE(brand_display_name, '') AS brand_display_name,
			COALESCE(litter_probability, 0.0) AS litter_probability,
			COALESCE(hazard_probability, 0.0) AS hazard_probability,
			COALESCE(digital_bug_probability, 0.0) AS digital_bug_probability,
			COALESCE(severity_level, 0.0) AS severity_level,
			COALESCE(summary, '') AS summary,
			COALESCE(language, '') AS language,
			COALESCE(classification, '') AS classification,
			is_valid,
			COALESCE(inferred_contact_emails, '') AS inferred_contact_emails,
			created_at, updated_at
		FROM report_analysis
		WHERE seq = ?
		ORDER BY (language = 'en') DESC, updated_at DESC, created_at DESC
	`, seq)
	if err != nil {
		return nil, fmt.Errorf("load analysis seq=%d: %w", seq, err)
	}
	defer rows.Close()

	var analyses []models.ReportAnalysis
	for rows.Next() {
		var analysis models.ReportAnalysis
		if err := rows.Scan(
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
			&analysis.IsValid,
			&analysis.InferredContactEmails,
			&analysis.CreatedAt,
			&analysis.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan analysis seq=%d: %w", seq, err)
		}
		analyses = append(analyses, analysis)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analysis seq=%d: %w", seq, err)
	}
	if len(analyses) == 0 {
		return nil, fmt.Errorf("no analysis rows found for seq=%d", seq)
	}

	if report.Timestamp.IsZero() {
		report.Timestamp = time.Now().UTC()
	}

	return &models.ReportWithAnalysis{
		Report:   report,
		Analysis: analyses,
	}, nil
}

// normalizeBrandName normalizes a brand name for consistent comparison
func normalizeBrandName(brandName string) string {
	if brandName == "" {
		return ""
	}

	// Convert to lowercase and remove common punctuation
	normalized := strings.ToLower(brandName)
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.ReplaceAll(normalized, "&", "")
	normalized = strings.ReplaceAll(normalized, "and", "")
	normalized = strings.Join(strings.Fields(normalized), "")

	return normalized
}
