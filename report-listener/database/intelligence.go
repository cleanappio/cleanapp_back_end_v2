package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type NamedCount struct {
	Name  string
	Count int
}

type IntelligenceContext struct {
	OrgID               string
	ReportsAnalyzed     int
	ReportsThisMonth    int
	HighPriorityCount   int
	MediumPriorityCount int
	TopClassifications  []NamedCount
	TopIssues           []NamedCount
	RecentSummaries     []string
}

func (d *Database) EnsureIntelligenceTables(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS intelligence_usage (
			session_id VARCHAR(128) PRIMARY KEY,
			turns_used INT NOT NULL DEFAULT 0,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to ensure intelligence_usage table: %w", err)
	}
	return nil
}

func (d *Database) GetAndIncrementIntelligenceUsage(ctx context.Context, sessionID string, maxTurns int, ttl time.Duration) (bool, int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, 0, fmt.Errorf("session_id is required")
	}
	if maxTurns <= 0 {
		return true, 0, nil
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to start tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	var turnsUsed int
	var expiresAt time.Time
	hasRow := true

	rowErr := tx.QueryRowContext(ctx, `
		SELECT turns_used, expires_at
		FROM intelligence_usage
		WHERE session_id = ?
		FOR UPDATE
	`, sessionID).Scan(&turnsUsed, &expiresAt)
	if rowErr == sql.ErrNoRows {
		hasRow = false
		turnsUsed = 0
		expiresAt = now.Add(ttl)
	} else if rowErr != nil {
		return false, 0, fmt.Errorf("failed to read intelligence usage: %w", rowErr)
	}

	expired := !hasRow || !expiresAt.After(now)
	if expired {
		turnsUsed = 0
		expiresAt = now.Add(ttl)
	}

	if turnsUsed >= maxTurns {
		if err := tx.Commit(); err != nil {
			return false, turnsUsed, fmt.Errorf("failed to commit usage tx: %w", err)
		}
		return false, turnsUsed, nil
	}

	turnsUsed++
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO intelligence_usage (session_id, turns_used, expires_at)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			turns_used = VALUES(turns_used),
			expires_at = VALUES(expires_at)
	`, sessionID, turnsUsed, expiresAt); err != nil {
		return false, 0, fmt.Errorf("failed to upsert intelligence usage: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("failed to commit usage tx: %w", err)
	}
	return true, turnsUsed, nil
}

func (d *Database) GetIntelligenceContext(ctx context.Context, orgID string) (*IntelligenceContext, error) {
	org := strings.ToLower(strings.TrimSpace(orgID))
	if org == "" {
		return nil, fmt.Errorf("org_id is required")
	}

	total, high, medium, err := d.GetBrandPriorityCountsByBrandName(ctx, org)
	if err != nil {
		return nil, err
	}

	result := &IntelligenceContext{
		OrgID:               org,
		ReportsAnalyzed:     total,
		HighPriorityCount:   high,
		MediumPriorityCount: medium,
	}

	_ = d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM reports r
		INNER JOIN report_analysis ra ON ra.seq = r.seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
		AND r.ts >= DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01 00:00:00')
	`, org).Scan(&result.ReportsThisMonth)

	classRows, err := d.db.QueryContext(ctx, `
		SELECT COALESCE(classification, 'unknown') AS classification, COUNT(*) AS cnt
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		GROUP BY classification
		ORDER BY cnt DESC
		LIMIT 5
	`, org)
	if err == nil {
		defer classRows.Close()
		for classRows.Next() {
			var item NamedCount
			if scanErr := classRows.Scan(&item.Name, &item.Count); scanErr == nil {
				result.TopClassifications = append(result.TopClassifications, item)
			}
		}
	}

	issueRows, err := d.db.QueryContext(ctx, `
		SELECT title, COUNT(*) AS cnt
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		AND title IS NOT NULL
		AND title != ''
		GROUP BY title
		ORDER BY cnt DESC
		LIMIT 5
	`, org)
	if err == nil {
		defer issueRows.Close()
		for issueRows.Next() {
			var item NamedCount
			if scanErr := issueRows.Scan(&item.Name, &item.Count); scanErr == nil {
				result.TopIssues = append(result.TopIssues, item)
			}
		}
	}

	summaryRows, err := d.db.QueryContext(ctx, `
		SELECT summary
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		AND summary IS NOT NULL
		AND summary != ''
		ORDER BY created_at DESC
		LIMIT 5
	`, org)
	if err == nil {
		defer summaryRows.Close()
		for summaryRows.Next() {
			var summary string
			if scanErr := summaryRows.Scan(&summary); scanErr == nil {
				result.RecentSummaries = append(result.RecentSummaries, summary)
			}
		}
	}

	return result, nil
}
