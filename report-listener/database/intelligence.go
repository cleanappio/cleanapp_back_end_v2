package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type NamedCount struct {
	Name  string
	Count int
}

type ReportSnippet struct {
	Seq            int
	Title          string
	Summary        string
	Classification string
	SeverityLevel  float64
	UpdatedAt      time.Time
}

type IntelligenceContext struct {
	OrgID                 string
	ReportsAnalyzed       int
	ReportsThisMonth      int
	ReportsLast30Days     int
	ReportsLast7Days      int
	ReportsPrev7Days      int
	GrowthLast7VsPrev7    float64
	HighPriorityCount     int
	MediumPriorityCount   int
	TopClassifications    []NamedCount
	TopIssues             []NamedCount
	RecentSummaries       []string
	RepresentativeReports []ReportSnippet
	MatchedReports        []ReportSnippet
	Keywords              []string
}

var keywordSplitRegex = regexp.MustCompile(`[^\p{L}\p{N}]+`)

var intelligenceStopWords = map[string]struct{}{
	"what": {}, "which": {}, "this": {}, "that": {}, "with": {}, "from": {}, "about": {},
	"show": {}, "give": {}, "please": {}, "could": {}, "would": {}, "there": {}, "their": {},
	"are": {}, "is": {}, "the": {}, "and": {}, "for": {}, "you": {}, "our": {}, "your": {},
	"have": {}, "has": {}, "were": {}, "been": {}, "into": {}, "over": {}, "under": {},
	"most": {}, "least": {}, "risks": {}, "risk": {}, "issues": {}, "issue": {}, "reports": {},
	"problem": {}, "problems": {}, "month": {}, "week": {}, "today": {}, "recent": {},
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

func (d *Database) GetIntelligenceContext(ctx context.Context, orgID, question string) (*IntelligenceContext, error) {
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
		SELECT
			COALESCE(SUM(CASE WHEN r.ts >= DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01 00:00:00') THEN 1 ELSE 0 END), 0) AS month_count,
			COALESCE(SUM(CASE WHEN r.ts >= UTC_TIMESTAMP() - INTERVAL 30 DAY THEN 1 ELSE 0 END), 0) AS last_30d_count,
			COALESCE(SUM(CASE WHEN r.ts >= UTC_TIMESTAMP() - INTERVAL 7 DAY THEN 1 ELSE 0 END), 0) AS last_7d_count,
			COALESCE(SUM(CASE WHEN r.ts < UTC_TIMESTAMP() - INTERVAL 7 DAY AND r.ts >= UTC_TIMESTAMP() - INTERVAL 14 DAY THEN 1 ELSE 0 END), 0) AS prev_7d_count
		FROM report_analysis ra
		INNER JOIN reports r ON ra.seq = r.seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
	`, org).Scan(
		&result.ReportsThisMonth,
		&result.ReportsLast30Days,
		&result.ReportsLast7Days,
		&result.ReportsPrev7Days,
	)

	if result.ReportsPrev7Days > 0 {
		result.GrowthLast7VsPrev7 = (float64(result.ReportsLast7Days-result.ReportsPrev7Days) / float64(result.ReportsPrev7Days)) * 100.0
	} else if result.ReportsLast7Days > 0 {
		result.GrowthLast7VsPrev7 = 100.0
	}

	classRows, err := d.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(classification, ''), 'unknown') AS classification, COUNT(*) AS cnt
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		GROUP BY classification
		ORDER BY cnt DESC
		LIMIT 6
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
		LIMIT 6
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
		LIMIT 8
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

	representative, repErr := d.getReportSnippets(ctx, org, nil, 6)
	if repErr == nil {
		result.RepresentativeReports = representative
	}

	keywords := extractKeywords(question)
	result.Keywords = keywords
	if len(keywords) > 0 {
		matched, matchErr := d.getReportSnippets(ctx, org, keywords, 5)
		if matchErr == nil {
			result.MatchedReports = mergeUniqueSnippets(matched, representative, 5)
		}
	}

	return result, nil
}

func (d *Database) getReportSnippets(ctx context.Context, org string, keywords []string, limit int) ([]ReportSnippet, error) {
	if limit <= 0 {
		limit = 3
	}

	query := `
		SELECT
			ra.seq,
			COALESCE(NULLIF(ra.title, ''), '(untitled report)') AS title,
			COALESCE(NULLIF(ra.summary, ''), COALESCE(NULLIF(ra.description, ''), '(no summary available)')) AS summary,
			COALESCE(NULLIF(ra.classification, ''), 'unknown') AS classification,
			COALESCE(ra.severity_level, 0) AS severity_level,
			COALESCE(ra.updated_at, ra.created_at, r.ts) AS updated_at
		FROM report_analysis ra
		INNER JOIN reports r ON r.seq = ra.seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
	`

	args := make([]interface{}, 0, 1+len(keywords)*3+1)
	args = append(args, org)

	if len(keywords) > 0 {
		clauses := make([]string, 0, len(keywords))
		for _, kw := range keywords {
			clauses = append(clauses, `(LOWER(ra.title) LIKE ? OR LOWER(ra.summary) LIKE ? OR LOWER(ra.description) LIKE ?)`)
			pattern := "%" + strings.ToLower(strings.TrimSpace(kw)) + "%"
			args = append(args, pattern, pattern, pattern)
		}
		query += " AND (" + strings.Join(clauses, " OR ") + ")"
	}

	query += `
		ORDER BY ra.severity_level DESC, updated_at DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ReportSnippet, 0, limit)
	for rows.Next() {
		var item ReportSnippet
		if scanErr := rows.Scan(&item.Seq, &item.Title, &item.Summary, &item.Classification, &item.SeverityLevel, &item.UpdatedAt); scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func extractKeywords(question string) []string {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return nil
	}

	raw := keywordSplitRegex.Split(q, -1)
	seen := make(map[string]struct{}, len(raw))
	keywords := make([]string, 0, 4)
	for _, token := range raw {
		t := strings.TrimSpace(token)
		if len(t) < 4 {
			continue
		}
		if _, stop := intelligenceStopWords[t]; stop {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		keywords = append(keywords, t)
		if len(keywords) == 4 {
			break
		}
	}
	return keywords
}

func mergeUniqueSnippets(primary, secondary []ReportSnippet, max int) []ReportSnippet {
	if max <= 0 {
		max = 3
	}
	out := make([]ReportSnippet, 0, max)
	seen := make(map[int]struct{}, max)
	appendUnique := func(items []ReportSnippet) {
		for _, item := range items {
			if len(out) >= max {
				return
			}
			if _, exists := seen[item.Seq]; exists {
				continue
			}
			seen[item.Seq] = struct{}{}
			out = append(out, item)
		}
	}
	appendUnique(primary)
	appendUnique(secondary)
	return out
}
