package database

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"report-listener/models"
)

const maxPublicNotificationCountReports = 50

// ICU's Unicode whitespace class matches Go strings.TrimSpace's blank-recipient
// rule, including tabs/newlines/NBSP. SQL TRIM alone only strips ASCII spaces.
const nonblankNotificationRecipientSQL = `REGEXP_LIKE(recipient_email, '[^[:space:]]')`

func reportNotificationCountQuery(seqs []int) (string, []interface{}, error) {
	if len(seqs) == 0 || len(seqs) > maxPublicNotificationCountReports {
		return "", nil, fmt.Errorf("notification count requires 1-%d selected reports", maxPublicNotificationCountReports)
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(seqs)), ",")
	args := make([]interface{}, 0, 2*len(seqs))
	for pass := 0; pass < 2; pass++ {
		for _, seq := range seqs {
			args = append(args, seq)
		}
	}
	// Both branches are restricted to the selected page before aggregation.
	// Only sequence and count cross the DB boundary, never recipient addresses.
	query := `SELECT notifications.seq, COUNT(*) AS email_count FROM (
 SELECT seq FROM report_email_delivery_events
 WHERE seq IN (` + marks + `) AND delivery_status = 'sent'
 AND ` + nonblankNotificationRecipientSQL + `
 UNION ALL
 SELECT old.seq FROM report_email_deliveries old
 WHERE old.seq IN (` + marks + `) AND old.delivery_status = 'sent'
 AND ` + nonblankNotificationRecipientSQL + `
 AND ` + reportNotificationLegacyUnrecordedSQL + `
 ) notifications GROUP BY notifications.seq`
	return query, args, nil
}

func knownPublicNotificationCount(count int) models.PublicNotificationCount {
	state := "none_recorded"
	if count > 0 {
		state = "sent"
	}
	return models.PublicNotificationCount{State: state, EmailCount: &count}
}

// ReportNotificationCounts is one bounded aggregate statement and therefore one
// consistent read of the event/legacy union. Call it after the report page's
// read-only snapshot commits: failure must not invalidate usable report data.
func (d *Database) ReportNotificationCounts(ctx context.Context, selected []int) (map[int]models.PublicNotificationCount, error) {
	seqs := make([]int, 0, len(selected))
	seen := map[int]bool{}
	for _, seq := range selected {
		if seq > 0 && !seen[seq] {
			seen[seq] = true
			seqs = append(seqs, seq)
		}
	}
	counts := make(map[int]models.PublicNotificationCount, len(seqs))
	if len(seqs) == 0 {
		return counts, nil
	}
	sort.Ints(seqs)
	query, args, err := reportNotificationCountQuery(seqs)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query report notification counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq, count int
		if err := rows.Scan(&seq, &count); err != nil {
			return nil, fmt.Errorf("scan report notification count: %w", err)
		}
		if !seen[seq] || count < 0 {
			return nil, fmt.Errorf("invalid selected report notification count")
		}
		counts[seq] = knownPublicNotificationCount(count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read report notification counts: %w", err)
	}
	for _, seq := range seqs {
		if _, exists := counts[seq]; !exists {
			counts[seq] = knownPublicNotificationCount(0)
		}
	}
	return counts, nil
}
