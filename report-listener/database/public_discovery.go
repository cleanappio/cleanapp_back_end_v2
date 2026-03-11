package database

import (
	"context"
	"fmt"
	"time"

	"report-listener/models"
)

const (
	publicDiscoveryQueryTimeout      = 8 * time.Second
	publicPointCandidateFloor        = 1000
	publicPointCandidateCeiling      = 8000
	publicPointCandidateMultiplier   = 2
	publicPointCandidateScanMultiple = 4
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

type publicPointCandidate struct {
	Seq       int
	PublicID  string
	Latitude  float64
	Longitude float64
}

func withPublicDiscoveryTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, publicDiscoveryQueryTimeout)
}

func clampPointCandidateBatchSize(limit int) int {
	size := limit * publicPointCandidateMultiplier
	if size < publicPointCandidateFloor {
		size = publicPointCandidateFloor
	}
	if size > publicPointCandidateCeiling {
		size = publicPointCandidateCeiling
	}
	return size
}

func clampPointCandidateScanCap(limit, batchSize int) int {
	scanCap := limit * publicPointCandidateScanMultiple
	if scanCap < batchSize {
		scanCap = batchSize
	}
	return scanCap
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

	queryCtx, cancel := withPublicDiscoveryTimeout(ctx)
	defer cancel()

	// Keep the public discovery summary path cheap: the map only needs brand-level
	// buckets, so avoid the status/owners joins that made the previous query stall.
	query := fmt.Sprintf(`
		SELECT
			ra.brand_name,
			COALESCE(NULLIF(MAX(ra.brand_display_name), ''), ra.brand_name) AS brand_display_name,
			COUNT(DISTINCT ra.seq) AS total
		FROM report_analysis ra
		LEFT JOIN report_raw rr ON ra.seq = rr.report_seq
		WHERE ra.language = ?
			AND ra.classification = ?
			AND ra.is_valid = TRUE
			AND ra.brand_name <> ''
			AND %s
		GROUP BY ra.brand_name
		ORDER BY total DESC, brand_display_name ASC
		LIMIT ?
	`, PublicVisibilityWhereSQL)

	rows, err := d.db.QueryContext(queryCtx, query, language, classification, limit)
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

func (d *Database) getPublicPhysicalPointCandidates(
	ctx context.Context,
	latMin float64,
	latMax float64,
	lonMin float64,
	lonMax float64,
	limit int,
	offset int,
) ([]publicPointCandidate, error) {
	queryCtx, cancel := withPublicDiscoveryTimeout(ctx)
	defer cancel()

	query := `
		SELECT
			r.seq,
			r.public_id,
			r.latitude,
			r.longitude
		FROM reports r FORCE INDEX (latitude_index)
		WHERE r.latitude BETWEEN ? AND ?
			AND r.longitude BETWEEN ? AND ?
			AND r.public_id <> ''
		ORDER BY r.seq DESC
		LIMIT ? OFFSET ?
	`

	rows, err := d.db.QueryContext(queryCtx, query, latMin, latMax, lonMin, lonMax, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query public physical point candidates: %w", err)
	}
	defer rows.Close()

	out := make([]publicPointCandidate, 0, limit)
	for rows.Next() {
		var item publicPointCandidate
		if err := rows.Scan(&item.Seq, &item.PublicID, &item.Latitude, &item.Longitude); err != nil {
			return nil, fmt.Errorf("failed to scan public physical point candidate: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public physical point candidates: %w", err)
	}

	return out, nil
}

func (d *Database) getPublicPhysicalSeverityBySeq(
	ctx context.Context,
	seqs []int,
) (map[int]float64, error) {
	if len(seqs) == 0 {
		return map[int]float64{}, nil
	}

	inClause, args := buildIntInClause(seqs)
	queryCtx, cancel := withPublicDiscoveryTimeout(ctx)
	defer cancel()

	query := fmt.Sprintf(`
		SELECT
			r.seq,
			COALESCE(MAX(ra.severity_level), 0.0) AS severity_level
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_status rs ON r.seq = rs.seq
		LEFT JOIN reports_owners ro ON r.seq = ro.seq
		LEFT JOIN report_raw rr ON r.seq = rr.report_seq
		WHERE r.seq IN (%s)
			AND ra.is_valid = TRUE
			AND ra.classification = 'physical'
			AND (rs.status IS NULL OR rs.status = 'active')
			AND %s
			AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		GROUP BY r.seq
	`, inClause, PublicVisibilityWhereSQL)

	rows, err := d.db.QueryContext(queryCtx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query public physical point severities: %w", err)
	}
	defer rows.Close()

	out := make(map[int]float64, len(seqs))
	for rows.Next() {
		var seq int
		var severity float64
		if err := rows.Scan(&seq, &severity); err != nil {
			return nil, fmt.Errorf("failed to scan public physical point severity: %w", err)
		}
		out[seq] = severity
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public physical point severities: %w", err)
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

	out := make([]PublicPointRecord, 0, limit)
	seen := make(map[int]struct{}, limit)
	candidateBatchSize := clampPointCandidateBatchSize(limit)
	scanCap := clampPointCandidateScanCap(limit, candidateBatchSize)

	for offset := 0; offset < scanCap && len(out) < limit; offset += candidateBatchSize {
		candidates, err := d.getPublicPhysicalPointCandidates(ctx, latMin, latMax, lonMin, lonMax, candidateBatchSize, offset)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			break
		}

		seqs := make([]int, 0, len(candidates))
		for _, candidate := range candidates {
			seqs = append(seqs, candidate.Seq)
		}

		severityBySeq, err := d.getPublicPhysicalSeverityBySeq(ctx, seqs)
		if err != nil {
			return nil, err
		}

		for _, candidate := range candidates {
			if _, exists := seen[candidate.Seq]; exists {
				continue
			}
			severity, ok := severityBySeq[candidate.Seq]
			if !ok {
				continue
			}
			seen[candidate.Seq] = struct{}{}
			out = append(out, PublicPointRecord{
				PublicID:      candidate.PublicID,
				Latitude:      candidate.Latitude,
				Longitude:     candidate.Longitude,
				SeverityLevel: severity,
			})
			if len(out) >= limit {
				break
			}
		}

		if len(candidates) < candidateBatchSize {
			break
		}
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
