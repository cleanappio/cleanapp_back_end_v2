package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"report-auth-service/models"
)

// ReportAuthService handles report authorization logic
type ReportAuthService struct {
	db *sql.DB
}

// NewReportAuthService creates a new report authorization service instance
func NewReportAuthService(db *sql.DB) *ReportAuthService {
	return &ReportAuthService{
		db: db,
	}
}

// CheckReportAuthorization checks if a user is authorized to view specific reports
func (s *ReportAuthService) CheckReportAuthorization(ctx context.Context, userID string, reportSeqs []int) ([]models.ReportAuthorization, error) {
	if len(reportSeqs) == 0 {
		return []models.ReportAuthorization{}, nil
	}

	// Build the IN clause for the query
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))

	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}

	// Query to get report information with location data and brand information
	query := fmt.Sprintf(`
		SELECT 
			r.seq,
			r.latitude,
			r.longitude,
			r.description,
			COALESCE(ra.brand_name, '') as brand_name
		FROM reports r
		LEFT JOIN report_analysis ra ON r.seq = ra.seq
		WHERE r.seq IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("ERROR: Failed to query reports for authorization: %v", err)
		return nil, fmt.Errorf("failed to query reports: %w", err)
	}
	defer rows.Close()

	var authorizations []models.ReportAuthorization
	reportMap := make(map[int]*models.ReportAuthorization)

	// Process each report
	for rows.Next() {
		var seq int
		var latitude, longitude float64
		var description sql.NullString
		var brandName sql.NullString

		if err := rows.Scan(&seq, &latitude, &longitude, &description, &brandName); err != nil {
			log.Printf("ERROR: Failed to scan report row: %v", err)
			return nil, fmt.Errorf("failed to scan report row: %w", err)
		}

		// Initialize authorization for this report
		auth := &models.ReportAuthorization{
			ReportSeq:  seq,
			Authorized: false,
		}

		// Check if the report location falls within any user's areas
		if err := s.checkLocationAuthorization(ctx, userID, latitude, longitude, auth); err != nil {
			log.Printf("ERROR: Failed to check location authorization for report %d: %v", seq, err)
			return nil, fmt.Errorf("failed to check location authorization: %w", err)
		}

		// Check brand authorization first (takes precedence)
		if brandName.Valid && brandName.String != "" {
			if err := s.checkBrandAuthorization(ctx, userID, brandName.String, auth); err != nil {
				log.Printf("ERROR: Failed to check brand authorization for report %d: %v", seq, err)
				return nil, fmt.Errorf("failed to check brand authorization: %w", err)
			}

			// If brand authorization denied, skip area check
			if !auth.Authorized {
				reportMap[seq] = auth
				continue
			}
		}

		// Check area authorization if brand authorization passed or wasn't required
		if err := s.checkLocationAuthorization(ctx, userID, latitude, longitude, auth); err != nil {
			log.Printf("ERROR: Failed to check location authorization for report %d: %v", seq, err)
			return nil, fmt.Errorf("failed to check location authorization: %w", err)
		}

		// If no area restrictions found, authorize the report
		if !auth.Authorized {
			auth.Authorized = true
			auth.Reason = "No area restrictions found"
		}

		reportMap[seq] = auth
	}

	// Create authorizations for all requested report seqs
	for _, seq := range reportSeqs {
		if auth, exists := reportMap[seq]; exists {
			authorizations = append(authorizations, *auth)
		} else {
			// Report not found
			authorizations = append(authorizations, models.ReportAuthorization{
				ReportSeq:  seq,
				Authorized: false,
				Reason:     "Report not found",
			})
		}
	}

	log.Printf("Authorizations: %+v", authorizations)
	return authorizations, nil
}

// checkLocationAuthorization checks if a user is authorized for a specific location
func (s *ReportAuthService) checkLocationAuthorization(ctx context.Context, userID string, latitude, longitude float64, auth *models.ReportAuthorization) error {
	// Check if the location falls within any user's areas using spatial queries
	var areaOwnerID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT DISTINCT ca.customer_id 
		FROM customer_areas ca
		JOIN areas a ON ca.area_id = a.id
		JOIN area_index ai ON a.id = ai.area_id
		WHERE ST_Contains(ai.geom, ST_Point(?, ?))
		LIMIT 1
	`, longitude, latitude).Scan(&areaOwnerID)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query area ownership: %w", err)
	}

	if err == sql.ErrNoRows {
		// No areas found containing this location - authorize
		auth.Authorized = true
		auth.Reason = "Location not within any restricted area"
		return nil
	}

	if areaOwnerID.Valid {
		if areaOwnerID.String == userID {
			// Area belongs to the logged-in user - authorize
			auth.Authorized = true
			auth.Reason = "Location within user's area"
		} else {
			// Area belongs to another user - deny
			auth.Authorized = false
			auth.Reason = "Location within another user's area"
		}
	}

	return nil
}

// checkBrandAuthorization checks if a user is authorized for a specific brand
func (s *ReportAuthService) checkBrandAuthorization(ctx context.Context, userID string, brandName string, auth *models.ReportAuthorization) error {
	if brandName == "" {
		return nil // No brand restriction
	}

	// Check if the brand belongs to any user
	var brandOwnerID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT customer_id FROM customer_brands WHERE brand_name = ? LIMIT 1",
		normalizeBrandName(brandName)).Scan(&brandOwnerID)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query brand ownership: %w", err)
	}

	if err == sql.ErrNoRows {
		// Brand doesn't belong to any user - authorize
		auth.Authorized = true
		auth.Reason = "Brand not restricted to any user"
		return nil
	}

	if brandOwnerID.Valid {
		if brandOwnerID.String == userID {
			// Brand belongs to the logged-in user - authorize
			auth.Authorized = true
			auth.Reason = "Brand belongs to user"
		} else {
			// Brand belongs to another user - deny
			auth.Authorized = false
			auth.Reason = "Brand belongs to another user"
		}
	}

	return nil
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
