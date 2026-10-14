package evidencebackfill

import (
	"cleanapp-common/evidence"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type DatabaseStore struct{ DB *sql.DB }

func (s DatabaseStore) Candidates(ctx context.Context, opts Options) ([]Candidate, error) {
	placeholders := make([]string, len(opts.Brands))
	args := []any{opts.AfterSeq}
	query := `SELECT ra.seq, MAX(ra.brand_name), COALESCE(MAX(CASE WHEN ra.language = 'en' THEN NULLIF(ra.title, '') END), MAX(NULLIF(ra.title, '')), '') FROM report_analysis ra LEFT JOIN report_evidence ev ON ev.seq = ra.seq WHERE ra.seq > ?`
	if opts.ThroughSeq > 0 {
		query += ` AND ra.seq <= ?`
		args = append(args, opts.ThroughSeq)
	}
	if opts.ShardCount > 1 {
		query += ` AND MOD(ra.seq, ?) = ?`
		args = append(args, opts.ShardCount, opts.ShardIndex)
	}
	for i, brand := range opts.Brands {
		placeholders[i] = "?"
		args = append(args, brand)
	}
	query += ` AND ra.brand_name IN (` + strings.Join(placeholders, ",") + `)`
	if !opts.Force {
		query += ` AND COALESCE(ev.evidence_version,0) < 1`
	}
	query += ` GROUP BY ra.seq ORDER BY ra.seq ASC LIMIT ?`
	args = append(args, opts.Limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select evidence backfill candidates (run explicit migrations first): %w", err)
	}
	defer rows.Close()
	candidates := []Candidate{}
	for rows.Next() {
		var row Candidate
		if err := rows.Scan(&row.Seq, &row.Brand, &row.Title); err != nil {
			return nil, err
		}
		candidates = append(candidates, row)
	}
	return candidates, rows.Err()
}
func (s DatabaseStore) Original(ctx context.Context, seq int) (evidence.Original, error) {
	return evidence.LoadOriginal(ctx, s.DB, seq)
}
func (s DatabaseStore) Save(ctx context.Context, seq int, fields evidence.Fields) error {
	return evidence.Save(ctx, s.DB, seq, fields, evidence.Version)
}
