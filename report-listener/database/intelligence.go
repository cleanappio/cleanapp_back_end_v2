package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type NamedCount struct {
	Name  string
	Count int
}

type SeverityDistribution struct {
	Critical int
	High     int
	Medium   int
	Low      int
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
	LastReportAt          *time.Time
	HighPriorityCount     int
	MediumPriorityCount   int
	SeverityDistribution  SeverityDistribution
	TopClassifications    []NamedCount
	TopIssues             []NamedCount
	TopEntities           []NamedCount
	TopTags               []NamedCount
	RecentSummaries       []string
	RepresentativeReports []ReportSnippet
	MatchedReports        []ReportSnippet
	EvidencePack          []ReportSnippet
	Keywords              []string
}

type IntelligenceContextOptions struct {
	Intent           string
	ExcludeReportIDs []int
}

type FixPriority struct {
	Issue       string
	Frequency   int
	AvgSeverity float64
	Recent7Days int
	Score       float64
	Reports     []ReportSnippet
}

var keywordSplitRegex = regexp.MustCompile(`[^\p{L}\p{N}]+`)
var issueNormalizeRegex = regexp.MustCompile(`[^\p{L}\p{N}]+`)

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
	_, err = d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS intelligence_session_state (
			session_id VARCHAR(128) PRIMARY KEY,
			last_report_ids_json TEXT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to ensure intelligence_session_state table: %w", err)
	}
	return nil
}

func (d *Database) GetLastReportIDsForSession(ctx context.Context, sessionID string) ([]int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}

	var raw sql.NullString
	var expiresAt time.Time
	err := d.db.QueryRowContext(ctx, `
		SELECT last_report_ids_json, expires_at
		FROM intelligence_session_state
		WHERE session_id = ?
	`, sessionID).Scan(&raw, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read intelligence session state: %w", err)
	}

	if !expiresAt.After(time.Now().UTC()) || !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil, nil
	}

	var ids []int
	if unmarshalErr := json.Unmarshal([]byte(raw.String), &ids); unmarshalErr != nil {
		return nil, nil
	}

	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func (d *Database) SaveLastReportIDsForSession(ctx context.Context, sessionID string, ids []int, ttl time.Duration) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	seen := make(map[int]struct{}, len(ids))
	clean := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
		if len(clean) >= 24 {
			break
		}
	}

	raw, err := json.Marshal(clean)
	if err != nil {
		return fmt.Errorf("failed to marshal intelligence session ids: %w", err)
	}
	expiresAt := time.Now().UTC().Add(ttl)

	_, err = d.db.ExecContext(ctx, `
		INSERT INTO intelligence_session_state (session_id, last_report_ids_json, expires_at)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			last_report_ids_json = VALUES(last_report_ids_json),
			expires_at = VALUES(expires_at)
	`, sessionID, string(raw), expiresAt)
	if err != nil {
		return fmt.Errorf("failed to upsert intelligence session state: %w", err)
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
	return d.GetIntelligenceContextWithOptions(ctx, orgID, question, IntelligenceContextOptions{})
}

func (d *Database) GetIntelligenceContextWithOptions(ctx context.Context, orgID, question string, opts IntelligenceContextOptions) (*IntelligenceContext, error) {
	org := strings.ToLower(strings.TrimSpace(orgID))
	if org == "" {
		return nil, fmt.Errorf("org_id is required")
	}

	total, high, medium := 0, 0, 0
	if t, h, m, err := d.GetBrandPriorityCountsByBrandName(ctx, org); err == nil {
		total, high, medium = t, h, m
	} else {
		// Do not fail intelligence requests for large brands if priority-count query times out.
		// Fall back to total-only count with a short bounded timeout.
		parent := ctx
		if parent.Err() != nil {
			parent = context.Background()
		}
		fallbackCtx, cancel := context.WithTimeout(parent, 8*time.Second)
		defer cancel()
		if tOnly, countErr := d.GetReportsCountByBrandName(fallbackCtx, org); countErr == nil {
			total = tOnly
		}
	}

	result := &IntelligenceContext{
		OrgID:               org,
		ReportsAnalyzed:     total,
		HighPriorityCount:   high,
		MediumPriorityCount: medium,
	}

	var lastReportAt sql.NullTime
	_ = d.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN r.ts >= DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01 00:00:00') THEN 1 ELSE 0 END), 0) AS month_count,
			COALESCE(SUM(CASE WHEN r.ts >= UTC_TIMESTAMP() - INTERVAL 30 DAY THEN 1 ELSE 0 END), 0) AS last_30d_count,
			COALESCE(SUM(CASE WHEN r.ts >= UTC_TIMESTAMP() - INTERVAL 7 DAY THEN 1 ELSE 0 END), 0) AS last_7d_count,
			COALESCE(SUM(CASE WHEN r.ts < UTC_TIMESTAMP() - INTERVAL 7 DAY AND r.ts >= UTC_TIMESTAMP() - INTERVAL 14 DAY THEN 1 ELSE 0 END), 0) AS prev_7d_count,
			COALESCE(SUM(CASE WHEN ra.severity_level >= 0.85 THEN 1 ELSE 0 END), 0) AS critical_count,
			COALESCE(SUM(CASE WHEN ra.severity_level >= 0.70 AND ra.severity_level < 0.85 THEN 1 ELSE 0 END), 0) AS high_count,
			COALESCE(SUM(CASE WHEN ra.severity_level >= 0.40 AND ra.severity_level < 0.70 THEN 1 ELSE 0 END), 0) AS medium_count,
			COALESCE(SUM(CASE WHEN ra.severity_level < 0.40 THEN 1 ELSE 0 END), 0) AS low_count,
			MAX(COALESCE(ra.updated_at, ra.created_at, r.ts)) AS last_report_at
		FROM report_analysis ra
		INNER JOIN reports r ON ra.seq = r.seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
	`, org).Scan(
		&result.ReportsThisMonth,
		&result.ReportsLast30Days,
		&result.ReportsLast7Days,
		&result.ReportsPrev7Days,
		&result.SeverityDistribution.Critical,
		&result.SeverityDistribution.High,
		&result.SeverityDistribution.Medium,
		&result.SeverityDistribution.Low,
		&lastReportAt,
	)
	if lastReportAt.Valid {
		t := lastReportAt.Time.UTC()
		result.LastReportAt = &t
	}

	if result.ReportsPrev7Days > 0 {
		result.GrowthLast7VsPrev7 = (float64(result.ReportsLast7Days-result.ReportsPrev7Days) / float64(result.ReportsPrev7Days)) * 100.0
	} else if result.ReportsLast7Days > 0 {
		result.GrowthLast7VsPrev7 = 100.0
	}

	result.TopClassifications, _ = d.getNamedCounts(ctx, `
		SELECT COALESCE(NULLIF(classification, ''), 'unknown') AS label, COUNT(*) AS cnt
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		GROUP BY classification
		ORDER BY cnt DESC
		LIMIT 6
	`, org)

	result.TopIssues, _ = d.getNamedCounts(ctx, `
		SELECT title AS label, COUNT(*) AS cnt
		FROM report_analysis
		WHERE brand_name = ?
		AND is_valid = TRUE
		AND title IS NOT NULL
		AND title != ''
		GROUP BY title
		ORDER BY cnt DESC
		LIMIT 6
	`, org)

	result.TopEntities, _ = d.getNamedCounts(ctx, `
		SELECT entity AS label, COUNT(*) AS cnt
		FROM (
			SELECT LOWER(TRIM(rd.company_name)) AS entity
			FROM report_details rd
			INNER JOIN report_analysis ra ON ra.seq = rd.seq
			WHERE ra.brand_name = ? AND ra.is_valid = TRUE AND rd.company_name IS NOT NULL AND rd.company_name != ''
			UNION ALL
			SELECT LOWER(TRIM(rd.product_name)) AS entity
			FROM report_details rd
			INNER JOIN report_analysis ra ON ra.seq = rd.seq
			WHERE ra.brand_name = ? AND ra.is_valid = TRUE AND rd.product_name IS NOT NULL AND rd.product_name != ''
		) t
		GROUP BY entity
		ORDER BY cnt DESC
		LIMIT 6
	`, org, org)

	result.TopTags, _ = d.getNamedCounts(ctx, `
		SELECT COALESCE(NULLIF(rt.tag, ''), 'unknown') AS label, COUNT(*) AS cnt
		FROM report_tags rt
		INNER JOIN report_analysis ra ON ra.seq = rt.report_seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
		GROUP BY rt.tag
		ORDER BY cnt DESC
		LIMIT 6
	`, org)

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

	keywords := extractKeywords(question)
	result.Keywords = keywords

	recentRelevant, _ := d.getReportSnippets(ctx, org, keywords, opts.ExcludeReportIDs, 8, "recent")
	severeRelevant, _ := d.getReportSnippets(ctx, org, keywords, opts.ExcludeReportIDs, 8, "severity")
	recurringRelevant, _ := d.getRecurringSnippets(ctx, org, keywords, result.TopIssues, opts.ExcludeReportIDs, 8)
	representative, _ := d.getReportSnippets(ctx, org, nil, opts.ExcludeReportIDs, 10, "severity")

	result.RepresentativeReports = representative
	switch strings.ToLower(strings.TrimSpace(opts.Intent)) {
	case "complaints_summary":
		result.EvidencePack = mergeEvidenceWithRules(recurringRelevant, recentRelevant, severeRelevant, representative, 6)
	case "fix_first":
		result.EvidencePack = mergeEvidenceWithRules(severeRelevant, recurringRelevant, recentRelevant, representative, 6)
	case "security_risks":
		result.EvidencePack = mergeEvidenceWithRules(severeRelevant, recentRelevant, recurringRelevant, representative, 6)
	case "trends":
		result.EvidencePack = mergeEvidenceWithRules(recentRelevant, recurringRelevant, severeRelevant, representative, 6)
	default:
		result.EvidencePack = mergeEvidenceWithRules(recentRelevant, severeRelevant, recurringRelevant, representative, 6)
	}
	result.MatchedReports = result.EvidencePack

	return result, nil
}

func (d *Database) getNamedCounts(ctx context.Context, query string, args ...interface{}) ([]NamedCount, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]NamedCount, 0, 6)
	for rows.Next() {
		var item NamedCount
		if scanErr := rows.Scan(&item.Name, &item.Count); scanErr != nil {
			return nil, scanErr
		}
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *Database) getReportSnippets(ctx context.Context, org string, keywords []string, excludeSeq []int, limit int, sortMode string) ([]ReportSnippet, error) {
	if limit <= 0 {
		limit = 3
	}

	orderBy := "updated_at DESC, ra.severity_level DESC"
	switch strings.ToLower(strings.TrimSpace(sortMode)) {
	case "severity":
		orderBy = "ra.severity_level DESC, updated_at DESC"
	case "recent":
		orderBy = "updated_at DESC, ra.severity_level DESC"
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
	if clause, clauseArgs := buildKeywordFilterClause(keywords); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildExcludeSeqClause(excludeSeq); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}

	query += " ORDER BY " + orderBy + " LIMIT ?"
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

func (d *Database) getRecurringSnippets(ctx context.Context, org string, keywords []string, topIssues []NamedCount, excludeSeq []int, limit int) ([]ReportSnippet, error) {
	if limit <= 0 {
		limit = 3
	}

	titles := make([]string, 0, 3)
	for _, issue := range topIssues {
		t := strings.TrimSpace(issue.Name)
		if t == "" {
			continue
		}
		titles = append(titles, t)
		if len(titles) == 3 {
			break
		}
	}
	if len(titles) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(titles))
	args := make([]interface{}, 0, 1+len(titles)+len(keywords)*3+1)
	for i := range titles {
		placeholders[i] = "?"
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
		AND ra.title IN (` + strings.Join(placeholders, ",") + `)
	`
	args = append(args, org)
	for _, title := range titles {
		args = append(args, title)
	}

	if clause, clauseArgs := buildKeywordFilterClause(keywords); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildExcludeSeqClause(excludeSeq); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}

	query += " ORDER BY updated_at DESC, ra.severity_level DESC LIMIT ?"
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

func buildKeywordFilterClause(keywords []string) (string, []interface{}) {
	if len(keywords) == 0 {
		return "", nil
	}
	clauses := make([]string, 0, len(keywords))
	args := make([]interface{}, 0, len(keywords)*3)
	for _, kw := range keywords {
		t := strings.ToLower(strings.TrimSpace(kw))
		if t == "" {
			continue
		}
		clauses = append(clauses, `(LOWER(ra.title) LIKE ? OR LOWER(ra.summary) LIKE ? OR LOWER(ra.description) LIKE ?)`)
		pattern := "%" + t + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " AND (" + strings.Join(clauses, " OR ") + ")", args
}

func buildExcludeSeqClause(excludeSeq []int) (string, []interface{}) {
	if len(excludeSeq) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(excludeSeq))
	args := make([]interface{}, 0, len(excludeSeq))
	seen := make(map[int]struct{}, len(excludeSeq))
	for _, seq := range excludeSeq {
		if seq <= 0 {
			continue
		}
		if _, ok := seen[seq]; ok {
			continue
		}
		seen[seq] = struct{}{}
		parts = append(parts, "?")
		args = append(args, seq)
		if len(parts) >= 64 {
			break
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " AND ra.seq NOT IN (" + strings.Join(parts, ",") + ")", args
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

func mergeEvidenceWithRules(recent, severe, recurring, fallback []ReportSnippet, max int) []ReportSnippet {
	if max <= 0 {
		max = 6
	}

	out := make([]ReportSnippet, 0, max)
	seenSeq := make(map[int]struct{}, max)
	seenIssue := make(map[string]struct{}, max)

	addOne := func(item ReportSnippet) bool {
		if len(out) >= max {
			return false
		}
		if _, exists := seenSeq[item.Seq]; exists {
			return false
		}
		key := normalizeIssueKey(item)
		if key != "" {
			if _, exists := seenIssue[key]; exists {
				return false
			}
			seenIssue[key] = struct{}{}
		}
		seenSeq[item.Seq] = struct{}{}
		out = append(out, item)
		return true
	}

	addFirstUnique := func(items []ReportSnippet) {
		for _, item := range items {
			if addOne(item) {
				return
			}
		}
	}

	// Required selection priority:
	// (a) most recent relevant, (b) highest severity relevant, (c) representative recurring issue.
	addFirstUnique(recent)
	addFirstUnique(severe)
	addFirstUnique(recurring)

	all := [][]ReportSnippet{recent, severe, recurring, fallback}
	for _, bucket := range all {
		for _, item := range bucket {
			if len(out) >= max {
				return out
			}
			addOne(item)
		}
	}

	return out
}

func (d *Database) GetFixPriorities(ctx context.Context, orgID, question string, excludeSeq []int, limit int) ([]FixPriority, error) {
	org := strings.ToLower(strings.TrimSpace(orgID))
	if org == "" {
		return nil, fmt.Errorf("org_id is required")
	}
	if limit <= 0 {
		limit = 3
	}

	keywords := extractKeywords(question)
	query := `
		SELECT
			COALESCE(NULLIF(ra.title, ''), '(untitled report)') AS issue_title,
			COUNT(*) AS freq,
			COALESCE(AVG(ra.severity_level), 0) AS avg_sev,
			COALESCE(SUM(CASE WHEN COALESCE(ra.updated_at, ra.created_at, r.ts) >= UTC_TIMESTAMP() - INTERVAL 7 DAY THEN 1 ELSE 0 END), 0) AS recent7
		FROM report_analysis ra
		INNER JOIN reports r ON r.seq = ra.seq
		WHERE ra.brand_name = ?
		AND ra.is_valid = TRUE
	`
	args := make([]interface{}, 0, 1+len(keywords)*3+len(excludeSeq)+1)
	args = append(args, org)

	if clause, clauseArgs := buildKeywordFilterClause(keywords); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}
	if clause, clauseArgs := buildExcludeSeqClause(excludeSeq); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}

	query += `
		GROUP BY issue_title
		HAVING issue_title != ''
		ORDER BY (
			(COALESCE(AVG(ra.severity_level), 0) + 0.1)
			* LOG(2 + COUNT(*))
			* (1 + COALESCE(SUM(CASE WHEN COALESCE(ra.updated_at, ra.created_at, r.ts) >= UTC_TIMESTAMP() - INTERVAL 7 DAY THEN 1 ELSE 0 END), 0) * 0.2)
		) DESC,
		MAX(COALESCE(ra.updated_at, ra.created_at, r.ts)) DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query fix priorities: %w", err)
	}
	defer rows.Close()

	priorities := make([]FixPriority, 0, limit)
	for rows.Next() {
		var p FixPriority
		if scanErr := rows.Scan(&p.Issue, &p.Frequency, &p.AvgSeverity, &p.Recent7Days); scanErr != nil {
			return nil, scanErr
		}
		if strings.TrimSpace(p.Issue) == "" {
			continue
		}
		recencyFactor := 1.0 + (float64(p.Recent7Days) * 0.2)
		if recencyFactor < 1.0 {
			recencyFactor = 1.0
		}
		p.Score = (p.AvgSeverity + 0.1) * float64(p.Frequency) * recencyFactor
		priorities = append(priorities, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range priorities {
		reports, repErr := d.getReportsForIssue(ctx, org, priorities[i].Issue, excludeSeq, 2)
		if repErr == nil {
			priorities[i].Reports = reports
		}
	}

	sort.SliceStable(priorities, func(i, j int) bool {
		if priorities[i].Score == priorities[j].Score {
			return priorities[i].Recent7Days > priorities[j].Recent7Days
		}
		return priorities[i].Score > priorities[j].Score
	})
	if len(priorities) > limit {
		priorities = priorities[:limit]
	}
	return priorities, nil
}

func (d *Database) getReportsForIssue(ctx context.Context, org, issue string, excludeSeq []int, limit int) ([]ReportSnippet, error) {
	if limit <= 0 {
		limit = 2
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
		AND COALESCE(NULLIF(ra.title, ''), '(untitled report)') = ?
	`
	args := []interface{}{org, issue}
	if clause, clauseArgs := buildExcludeSeqClause(excludeSeq); clause != "" {
		query += clause
		args = append(args, clauseArgs...)
	}
	query += ` ORDER BY ra.severity_level DESC, COALESCE(ra.updated_at, ra.created_at, r.ts) DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ReportSnippet, 0, limit)
	for rows.Next() {
		var item ReportSnippet
		if scanErr := rows.Scan(&item.Seq, &item.Title, &item.Summary, &item.Classification, &item.SeverityLevel, &item.UpdatedAt); scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeIssueKey(item ReportSnippet) string {
	base := strings.TrimSpace(item.Title)
	if base == "" || strings.EqualFold(base, "(untitled report)") {
		base = strings.TrimSpace(item.Summary)
	}
	if base == "" {
		return ""
	}
	base = strings.ToLower(issueNormalizeRegex.ReplaceAllString(base, " "))
	base = strings.TrimSpace(strings.Join(strings.Fields(base), " "))
	if len(base) > 96 {
		base = base[:96]
	}
	return base
}
