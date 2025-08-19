package database

import (
	"slices"
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ca.customer_id 
		FROM customer_areas ca
		JOIN areas a ON ca.area_id = a.id
		JOIN area_index ai ON a.id = ai.area_id
		WHERE ST_Contains(ai.geom, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
	`, longitude, latitude)
	if err != nil {
		return fmt.Errorf("failed to query area ownership: %w", err)
	}
	defer rows.Close()

	var areaUserIDs []string
	for rows.Next() {
		var customerID string
		if err := rows.Scan(&customerID); err != nil {
			log.Printf("ERROR: Failed to scan area customer ID: %v", err)
			continue
		}
		areaUserIDs = append(areaUserIDs, customerID)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate area ownership rows: %w", err)
	}

	if len(areaUserIDs) == 0 {
		// No areas found containing this location - authorize
		auth.Authorized = true
		auth.Reason = "Location not within any restricted area"
		return nil
	}

	log.Printf("DEBUG: Location (%.6f, %.6f) is within areas owned by users: %v, current user %s", latitude, longitude, areaUserIDs, userID)

	// Check if current user is in the list of area owners
	userAuthorized := slices.Contains(areaUserIDs, userID)

	if userAuthorized {
		// Current user is one of the area owners - authorize
		auth.Authorized = true
		auth.Reason = "Location within user's area"
	} else {
		// Current user is not an area owner - deny
		auth.Authorized = false
		auth.Reason = "Location within another user's area"
	}

	return nil
}

// checkBrandAuthorization checks if a user is authorized for a specific brand
func (s *ReportAuthService) checkBrandAuthorization(ctx context.Context, userID string, brandName string, auth *models.ReportAuthorization) error {
	if brandName == "" {
		return nil // No brand restriction
	}

	// Fetch all user IDs for this brand
	rows, err := s.db.QueryContext(ctx,
		"SELECT customer_id FROM customer_brands WHERE brand_name = ?",
		normalizeBrandName(brandName))
	if err != nil {
		return fmt.Errorf("failed to query brand ownership: %w", err)
	}
	defer rows.Close()

	var brandUserIDs []string
	for rows.Next() {
		var customerID string
		if err := rows.Scan(&customerID); err != nil {
			log.Printf("ERROR: Failed to scan brand customer ID: %v", err)
			continue
		}
		brandUserIDs = append(brandUserIDs, customerID)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate brand ownership rows: %w", err)
	}

	if len(brandUserIDs) == 0 {
		// Brand doesn't belong to any user - authorize
		auth.Authorized = true
		auth.Reason = "Brand not restricted to any user"
		return nil
	}

	log.Printf("DEBUG: Brand %s belongs to users: %v, current user %s", brandName, brandUserIDs, userID)

	// Check if current user is in the list of brand owners
	userAuthorized := slices.Contains(brandUserIDs, userID)

	if userAuthorized {
		// Current user is one of the brand owners - authorize
		auth.Authorized = true
		auth.Reason = "Brand belongs to user"
	} else {
		// Current user is not a brand owner - deny
		auth.Authorized = false
		auth.Reason = "Brand belongs to other users"
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
