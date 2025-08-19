package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

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

// GetUnprocessedReports retrieves reports with their analysis that haven't been processed for ownership
func (s *OwnershipService) GetUnprocessedReports(ctx context.Context, batchSize int) ([]models.ReportWithAnalysis, error) {
	query := `
		SELECT DISTINCT 
			r.seq, r.ts, r.id, r.latitude, r.longitude,
			COALESCE(ra.brand_name, '') as brand_name,
			COALESCE(ra.brand_display_name, '') as brand_display_name
		FROM reports r
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		JOIN report_analysis ra ON r.seq = ra.seq
		WHERE ro.seq IS NULL
		AND ra.language = 'en'
		ORDER BY r.seq ASC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to query unprocessed reports: %w", err)
	}
	defer rows.Close()

	var reports []models.ReportWithAnalysis
	for rows.Next() {
		var report models.ReportWithAnalysis
		err := rows.Scan(
			&report.Report.Seq,
			&report.Report.Timestamp,
			&report.Report.ID,
			&report.Report.Latitude,
			&report.Report.Longitude,
			&report.Analysis.BrandName,
			&report.Analysis.BrandDisplayName,
		)
		if err != nil {
			log.Printf("ERROR: Failed to scan report: %v", err)
			continue
		}
		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reports: %w", err)
	}

	return reports, nil
}

// DetermineLocationOwners determines owners based on report location
func (s *OwnershipService) DetermineLocationOwners(ctx context.Context, latitude, longitude float64) ([]string, error) {
	query := `
		SELECT DISTINCT ca.customer_id
		FROM customer_areas ca
		JOIN areas a ON ca.area_id = a.id
		JOIN area_index ai ON a.id = ai.area_id
		WHERE ST_Contains(ai.geom, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
	`

	rows, err := s.db.QueryContext(ctx, query, longitude, latitude)
	if err != nil {
		return nil, fmt.Errorf("failed to query location ownership: %w", err)
	}
	defer rows.Close()

	var owners []string
	for rows.Next() {
		var customerID string
		if err := rows.Scan(&customerID); err != nil {
			log.Printf("ERROR: Failed to scan customer ID: %v", err)
			continue
		}
		owners = append(owners, customerID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating location ownership: %w", err)
	}

	return owners, nil
}

// DetermineBrandOwners determines owners based on report brand
func (s *OwnershipService) DetermineBrandOwners(ctx context.Context, brandName string) ([]string, error) {
	if brandName == "" {
		return []string{}, nil
	}

	normalizedBrand := normalizeBrandName(brandName)
	query := `
		SELECT DISTINCT customer_id
		FROM customer_brands
		WHERE brand_name = ?
	`

	rows, err := s.db.QueryContext(ctx, query, normalizedBrand)
	if err != nil {
		return nil, fmt.Errorf("failed to query brand ownership: %w", err)
	}
	defer rows.Close()

	var owners []string
	for rows.Next() {
		var customerID string
		if err := rows.Scan(&customerID); err != nil {
			log.Printf("ERROR: Failed to scan customer ID: %v", err)
			continue
		}
		owners = append(owners, customerID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating brand ownership: %w", err)
	}

	return owners, nil
}

// StoreReportOwners stores ownership information for a report
func (s *OwnershipService) StoreReportOwners(ctx context.Context, seq int, owners []string) error {
	if len(owners) == 0 {
		return nil // No owners to store
	}

	// Build the INSERT statement
	placeholders := make([]string, len(owners))
	args := make([]interface{}, len(owners)*2)

	for i, owner := range owners {
		placeholders[i] = "(?, ?)"
		args[i*2] = seq
		args[i*2+1] = owner
	}

	query := fmt.Sprintf(`
		INSERT IGNORE INTO reports_owners (seq, owner)
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
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT seq) FROM reports_owners").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total processed reports: %w", err)
	}
	return count, nil
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
