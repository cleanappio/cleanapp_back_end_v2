package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type FetcherPromotionRequestV1 struct {
	ID int64

	FetcherID string
	Status    string

	ContactEmail   sql.NullString
	VerifiedDomain sql.NullString

	RequestedTier              sql.NullInt64
	RequestedDailyCapItems     sql.NullInt64
	RequestedPerMinuteCapItems sql.NullInt64
	RequestedDefaultVisibility sql.NullString
	RequestedDefaultTrustLevel sql.NullString
	RequestedRoutingEnabled    sql.NullBool
	RequestedRewardsEnabled    sql.NullBool

	Notes         sql.NullString
	DecisionNotes sql.NullString
	ReviewedBy    sql.NullString
	ReviewedAt    sql.NullTime

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (d *Database) GetLatestPromotionRequestV1(ctx context.Context, fetcherID string) (*FetcherPromotionRequestV1, error) {
	var r FetcherPromotionRequestV1
	err := d.db.QueryRowContext(ctx, `
		SELECT
			id, fetcher_id, status,
			contact_email, verified_domain,
			requested_tier, requested_daily_cap_items, requested_per_minute_cap_items,
			requested_default_visibility, requested_default_trust_level,
			requested_routing_enabled, requested_rewards_enabled,
			notes, decision_notes, reviewed_by, reviewed_at,
			created_at, updated_at
		FROM fetcher_promotion_requests
		WHERE fetcher_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, fetcherID).Scan(
		&r.ID,
		&r.FetcherID,
		&r.Status,
		&r.ContactEmail,
		&r.VerifiedDomain,
		&r.RequestedTier,
		&r.RequestedDailyCapItems,
		&r.RequestedPerMinuteCapItems,
		&r.RequestedDefaultVisibility,
		&r.RequestedDefaultTrustLevel,
		&r.RequestedRoutingEnabled,
		&r.RequestedRewardsEnabled,
		&r.Notes,
		&r.DecisionNotes,
		&r.ReviewedBy,
		&r.ReviewedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get latest promotion request: %w", err)
	}
	return &r, nil
}

func (d *Database) GetPromotionRequestByIDV1(ctx context.Context, id int64) (*FetcherPromotionRequestV1, error) {
	var r FetcherPromotionRequestV1
	err := d.db.QueryRowContext(ctx, `
		SELECT
			id, fetcher_id, status,
			contact_email, verified_domain,
			requested_tier, requested_daily_cap_items, requested_per_minute_cap_items,
			requested_default_visibility, requested_default_trust_level,
			requested_routing_enabled, requested_rewards_enabled,
			notes, decision_notes, reviewed_by, reviewed_at,
			created_at, updated_at
		FROM fetcher_promotion_requests
		WHERE id = ?
	`, id).Scan(
		&r.ID,
		&r.FetcherID,
		&r.Status,
		&r.ContactEmail,
		&r.VerifiedDomain,
		&r.RequestedTier,
		&r.RequestedDailyCapItems,
		&r.RequestedPerMinuteCapItems,
		&r.RequestedDefaultVisibility,
		&r.RequestedDefaultTrustLevel,
		&r.RequestedRoutingEnabled,
		&r.RequestedRewardsEnabled,
		&r.Notes,
		&r.DecisionNotes,
		&r.ReviewedBy,
		&r.ReviewedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get promotion request: %w", err)
	}
	return &r, nil
}

func (d *Database) ListPromotionRequestsV1(ctx context.Context, status string, limit int) ([]FetcherPromotionRequestV1, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if status == "" {
		status = "pending"
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			id, fetcher_id, status,
			contact_email, verified_domain,
			requested_tier, requested_daily_cap_items, requested_per_minute_cap_items,
			requested_default_visibility, requested_default_trust_level,
			requested_routing_enabled, requested_rewards_enabled,
			notes, decision_notes, reviewed_by, reviewed_at,
			created_at, updated_at
		FROM fetcher_promotion_requests
		WHERE status = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list promotion requests: %w", err)
	}
	defer rows.Close()

	var out []FetcherPromotionRequestV1
	for rows.Next() {
		var r FetcherPromotionRequestV1
		if err := rows.Scan(
			&r.ID,
			&r.FetcherID,
			&r.Status,
			&r.ContactEmail,
			&r.VerifiedDomain,
			&r.RequestedTier,
			&r.RequestedDailyCapItems,
			&r.RequestedPerMinuteCapItems,
			&r.RequestedDefaultVisibility,
			&r.RequestedDefaultTrustLevel,
			&r.RequestedRoutingEnabled,
			&r.RequestedRewardsEnabled,
			&r.Notes,
			&r.DecisionNotes,
			&r.ReviewedBy,
			&r.ReviewedAt,
			&r.CreatedAt,
			&r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Database) CreatePromotionRequestV1(ctx context.Context, r *FetcherPromotionRequestV1) (int64, error) {
	if r == nil || r.FetcherID == "" {
		return 0, fmt.Errorf("missing fetcher_id")
	}

	// Single pending request at a time (MVP).
	var existingID int64
	err := d.db.QueryRowContext(ctx, `
		SELECT id FROM fetcher_promotion_requests
		WHERE fetcher_id = ? AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT 1
	`, r.FetcherID).Scan(&existingID)
	if err == nil && existingID > 0 {
		return 0, fmt.Errorf("pending request already exists")
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("check pending request: %w", err)
	}

	res, err := d.db.ExecContext(ctx, `
		INSERT INTO fetcher_promotion_requests (
			fetcher_id, status,
			contact_email, verified_domain,
			requested_tier, requested_daily_cap_items, requested_per_minute_cap_items,
			requested_default_visibility, requested_default_trust_level,
			requested_routing_enabled, requested_rewards_enabled,
			notes
		) VALUES (?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.FetcherID,
		nullableStr(r.ContactEmail.String),
		nullableStr(r.VerifiedDomain.String),
		nullableInt64(r.RequestedTier),
		nullableInt64(r.RequestedDailyCapItems),
		nullableInt64(r.RequestedPerMinuteCapItems),
		nullableStr(r.RequestedDefaultVisibility.String),
		nullableStr(r.RequestedDefaultTrustLevel.String),
		nullableBool(r.RequestedRoutingEnabled),
		nullableBool(r.RequestedRewardsEnabled),
		nullableStr(r.Notes.String),
	)
	if err != nil {
		return 0, fmt.Errorf("insert promotion request: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func nullableInt64(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullableBool(v sql.NullBool) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Bool
}
