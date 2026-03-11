package database

import (
	"context"
	"fmt"

	"report-listener/models"
)

type PublicPointRecord struct {
	PublicID      string
	Latitude      float64
	Longitude     float64
	SeverityLevel float64
}

type PublicBrandSummaryRecord struct {
	BrandName      string
	BrandDisplay   string
	Total          int
	Classification string
}

func (d *Database) GetPublicBrandSummaries(
	ctx context.Context,
	classification string,
	language string,
	limit int,
) ([]PublicBrandSummaryRecord, error) {
	if limit <= 0 {
		limit = 200
	}

	query := fmt.Sprintf(`
		SELECT
			ra.brand_name,
			COALESCE(NULLIF(ra.brand_display_name, ''), ra.brand_name) AS brand_display_name,
			COUNT(DISTINCT ra.seq) AS total
		FROM report_analysis ra
		LEFT JOIN report_status rs ON ra.seq = rs.seq
		LEFT JOIN reports_owners ro ON ra.seq = ro.seq
		LEFT JOIN report_raw rr ON ra.seq = rr.report_seq
		WHERE ra.language = ?
			AND ra.classification = ?
			AND ra.is_valid = TRUE
			AND ra.brand_name <> ''
			AND (rs.status IS NULL OR rs.status = 'active')
			AND %s
			AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		GROUP BY ra.brand_name, brand_display_name
		ORDER BY total DESC, brand_display_name ASC
		LIMIT ?
	`, PublicVisibilityWhereSQL)

	rows, err := d.db.QueryContext(ctx, query, language, classification, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query public brand summaries: %w", err)
	}
	defer rows.Close()

	out := make([]PublicBrandSummaryRecord, 0, limit)
	for rows.Next() {
		var item PublicBrandSummaryRecord
		item.Classification = classification
		if err := rows.Scan(&item.BrandName, &item.BrandDisplay, &item.Total); err != nil {
			return nil, fmt.Errorf("failed to scan public brand summary: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public brand summaries: %w", err)
	}

	return out, nil
}

func (d *Database) GetPublicPhysicalPointsByBBox(
	ctx context.Context,
	latMin float64,
	latMax float64,
	lonMin float64,
	lonMax float64,
	limit int,
) ([]PublicPointRecord, error) {
	if limit <= 0 {
		limit = 4000
	}

	query := fmt.Sprintf(`
		SELECT
			r.public_id,
			r.latitude,
			r.longitude,
			COALESCE(MAX(ra.severity_level), 0.0) AS severity_level
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		LEFT JOIN report_raw rr ON r.seq = rr.report_seq
		WHERE ra.is_valid = TRUE
			AND ra.classification = 'physical'
			AND (rs.status IS NULL OR rs.status = 'active')
			AND %s
			AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
			AND r.latitude BETWEEN ? AND ?
			AND r.longitude BETWEEN ? AND ?
		GROUP BY r.seq, r.public_id, r.latitude, r.longitude
		ORDER BY r.seq DESC
		LIMIT ?
	`, PublicVisibilityWhereSQL)

	rows, err := d.db.QueryContext(ctx, query, latMin, latMax, lonMin, lonMax, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query public physical points: %w", err)
	}
	defer rows.Close()

	out := make([]PublicPointRecord, 0, limit)
	for rows.Next() {
		var item PublicPointRecord
		if err := rows.Scan(&item.PublicID, &item.Latitude, &item.Longitude, &item.SeverityLevel); err != nil {
			return nil, fmt.Errorf("failed to scan public physical point: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public physical points: %w", err)
	}

	return out, nil
}

func PreferredMinimalAnalysis(
	analyses []models.MinimalAnalysis,
	language string,
) models.MinimalAnalysis {
	if len(analyses) == 0 {
		return models.MinimalAnalysis{}
	}
	for _, analysis := range analyses {
		if analysis.Language == language {
			return analysis
		}
	}
	return analyses[0]
}
