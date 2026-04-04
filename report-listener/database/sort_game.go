package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"report-listener/models"
)

const (
	sortVerdictHighValue = "high_value"
	sortVerdictSpam      = "spam"
	sortRewardKITNs      = 1
)

var (
	ErrNoSortableReports = errors.New("no sortable reports available")
	ErrDuplicateSortVote = errors.New("report already sorted by this user")
	ErrOwnReportSort     = errors.New("cannot sort own report")
	ErrInvalidSortVote   = errors.New("invalid sort vote")
	ErrSortTargetMissing = errors.New("sort target report not found")
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func normalizeSortVerdict(verdict string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case sortVerdictHighValue:
		return sortVerdictHighValue, nil
	case sortVerdictSpam:
		return sortVerdictSpam, nil
	default:
		return "", ErrInvalidSortVote
	}
}

func secureRandomOffset(count int) (int, error) {
	if count <= 1 {
		return 0, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func (d *Database) GetNextSortableReport(ctx context.Context, sorterID string) (*models.SortableReport, error) {
	sorterID = strings.TrimSpace(sorterID)
	if sorterID == "" {
		return nil, ErrInvalidSortVote
	}

	const countQuery = `
		SELECT COUNT(DISTINCT r.seq)
		FROM reports r
		LEFT JOIN report_raw rr ON rr.report_seq = r.seq
		LEFT JOIN report_status rs ON rs.seq = r.seq
		LEFT JOIN reports_owners ro ON ro.seq = r.seq
		WHERE EXISTS (
			SELECT 1
			FROM report_analysis ra
			WHERE ra.seq = r.seq
			  AND ra.is_valid = TRUE
			  AND ra.classification = 'physical'
		)
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ` + PublicVisibilityWhereSQL + `
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		AND r.id <> ?
		AND NOT EXISTS (
			SELECT 1
			FROM report_sort_events rse
			WHERE rse.report_seq = r.seq
			  AND rse.sorter_id = ?
		)
	`

	var total int
	if err := d.db.QueryRowContext(ctx, countQuery, sorterID, sorterID).Scan(&total); err != nil {
		return nil, fmt.Errorf("count sortable reports: %w", err)
	}
	if total == 0 {
		return nil, ErrNoSortableReports
	}

	offset, err := secureRandomOffset(total)
	if err != nil {
		return nil, fmt.Errorf("pick sortable report offset: %w", err)
	}

	const candidateQuery = `
		SELECT DISTINCT
			r.seq,
			r.public_id,
			r.ts,
			r.id,
			r.team,
			r.latitude,
			r.longitude,
			COALESCE(r.x, 0),
			COALESCE(r.y, 0),
			COALESCE(rsm.sort_count, 0),
			COALESCE(rsm.high_value_count, 0),
			COALESCE(rsm.spam_count, 0),
			COALESCE(rsm.urgency_sum, 0),
			COALESCE(CAST(rsm.urgency_mean AS DOUBLE), 0),
			rsm.last_sorted_at
		FROM reports r
		LEFT JOIN report_raw rr ON rr.report_seq = r.seq
		LEFT JOIN report_status rs ON rs.seq = r.seq
		LEFT JOIN reports_owners ro ON ro.seq = r.seq
		LEFT JOIN report_sort_metrics rsm ON rsm.report_seq = r.seq
		WHERE EXISTS (
			SELECT 1
			FROM report_analysis ra
			WHERE ra.seq = r.seq
			  AND ra.is_valid = TRUE
			  AND ra.classification = 'physical'
		)
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ` + PublicVisibilityWhereSQL + `
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		AND r.id <> ?
		AND NOT EXISTS (
			SELECT 1
			FROM report_sort_events rse
			WHERE rse.report_seq = r.seq
			  AND rse.sorter_id = ?
		)
		ORDER BY r.ts DESC
		LIMIT 1 OFFSET ?
	`

	var (
		candidate    models.SortableReport
		lastSortedAt sql.NullTime
	)
	err = d.db.QueryRowContext(ctx, candidateQuery, sorterID, sorterID, offset).Scan(
		&candidate.Report.Seq,
		&candidate.Report.PublicID,
		&candidate.Report.Timestamp,
		&candidate.Report.ID,
		&candidate.Report.Team,
		&candidate.Report.Latitude,
		&candidate.Report.Longitude,
		&candidate.Report.X,
		&candidate.Report.Y,
		&candidate.SortMetrics.SortCount,
		&candidate.SortMetrics.HighValueCount,
		&candidate.SortMetrics.SpamCount,
		&candidate.SortMetrics.UrgencySum,
		&candidate.SortMetrics.UrgencyMean,
		&lastSortedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoSortableReports
		}
		return nil, fmt.Errorf("query sortable report: %w", err)
	}
	candidate.SortMetrics.ReportSeq = candidate.Report.Seq
	if lastSortedAt.Valid {
		ts := lastSortedAt.Time
		candidate.SortMetrics.LastSortedAt = &ts
	}

	return &candidate, nil
}

func (d *Database) validateSortTarget(ctx context.Context, tx *sql.Tx, sorterID string, reportSeq int) error {
	const query = `
		SELECT r.id
		FROM reports r
		LEFT JOIN report_raw rr ON rr.report_seq = r.seq
		LEFT JOIN report_status rs ON rs.seq = r.seq
		LEFT JOIN reports_owners ro ON ro.seq = r.seq
		WHERE r.seq = ?
		AND EXISTS (
			SELECT 1
			FROM report_analysis ra
			WHERE ra.seq = r.seq
			  AND ra.is_valid = TRUE
			  AND ra.classification = 'physical'
		)
		AND (rs.status IS NULL OR rs.status = 'active')
		AND ` + PublicVisibilityWhereSQL + `
		AND (ro.owner IS NULL OR ro.owner = '' OR ro.is_public = TRUE)
		LIMIT 1
	`

	var ownerID string
	if err := tx.QueryRowContext(ctx, query, reportSeq).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSortTargetMissing
		}
		return fmt.Errorf("validate sort target: %w", err)
	}
	if strings.TrimSpace(ownerID) == sorterID {
		return ErrOwnReportSort
	}
	return nil
}

func loadReportSortMetrics(ctx context.Context, querier queryRower, reportSeq int) (models.ReportSortMetrics, error) {
	metrics := models.ReportSortMetrics{ReportSeq: reportSeq}
	var lastSortedAt sql.NullTime
	err := querier.QueryRowContext(ctx, `
		SELECT
			report_seq,
			sort_count,
			high_value_count,
			spam_count,
			urgency_sum,
			COALESCE(CAST(urgency_mean AS DOUBLE), 0),
			last_sorted_at
		FROM report_sort_metrics
		WHERE report_seq = ?
	`, reportSeq).Scan(
		&metrics.ReportSeq,
		&metrics.SortCount,
		&metrics.HighValueCount,
		&metrics.SpamCount,
		&metrics.UrgencySum,
		&metrics.UrgencyMean,
		&lastSortedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return metrics, nil
		}
		return metrics, err
	}
	if lastSortedAt.Valid {
		ts := lastSortedAt.Time
		metrics.LastSortedAt = &ts
	}
	return metrics, nil
}

func (d *Database) SubmitReportSort(ctx context.Context, vote models.ReportSortVote) (*models.ReportSortSubmissionResult, error) {
	vote.SorterID = strings.TrimSpace(vote.SorterID)
	if vote.SorterID == "" || vote.ReportSeq <= 0 {
		return nil, ErrInvalidSortVote
	}
	normalizedVerdict, err := normalizeSortVerdict(vote.Verdict)
	if err != nil {
		return nil, err
	}
	if vote.UrgencyScore < 0 || vote.UrgencyScore > 10 {
		return nil, ErrInvalidSortVote
	}
	if vote.CreatedAt.IsZero() {
		vote.CreatedAt = time.Now().UTC()
	}

	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin sort submission transaction: %w", err)
	}
	defer tx.Rollback()

	if err := d.validateSortTarget(ctx, tx, vote.SorterID, vote.ReportSeq); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO report_sort_events (
			report_seq,
			sorter_id,
			verdict,
			urgency_score,
			reward_kitns,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, vote.ReportSeq, vote.SorterID, normalizedVerdict, vote.UrgencyScore, sortRewardKITNs, vote.CreatedAt); err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, ErrDuplicateSortVote
		}
		return nil, fmt.Errorf("insert report sort event: %w", err)
	}

	highValueCount := 0
	spamCount := 0
	if normalizedVerdict == sortVerdictHighValue {
		highValueCount = 1
	} else {
		spamCount = 1
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO report_sort_metrics (
			report_seq,
			sort_count,
			high_value_count,
			spam_count,
			urgency_sum,
			urgency_mean,
			last_sorted_at
		) VALUES (?, 1, ?, ?, ?, ?, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE
			sort_count = sort_count + VALUES(sort_count),
			high_value_count = high_value_count + VALUES(high_value_count),
			spam_count = spam_count + VALUES(spam_count),
			urgency_sum = urgency_sum + VALUES(urgency_sum),
			urgency_mean = (urgency_sum + VALUES(urgency_sum)) / NULLIF(sort_count + VALUES(sort_count), 0),
			last_sorted_at = UTC_TIMESTAMP()
	`, vote.ReportSeq, highValueCount, spamCount, vote.UrgencyScore, float64(vote.UrgencyScore)); err != nil {
		return nil, fmt.Errorf("upsert report sort metrics: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, kitns_daily)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE kitns_daily = kitns_daily + VALUES(kitns_daily)
	`, vote.SorterID, sortRewardKITNs); err != nil {
		return nil, fmt.Errorf("credit user kitns: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users_shadow (id, kitns_daily)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE kitns_daily = kitns_daily + VALUES(kitns_daily)
	`, vote.SorterID, sortRewardKITNs); err != nil {
		return nil, fmt.Errorf("credit user shadow kitns: %w", err)
	}

	metrics, err := loadReportSortMetrics(ctx, tx, vote.ReportSeq)
	if err != nil {
		return nil, fmt.Errorf("load report sort metrics: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit sort submission: %w", err)
	}

	return &models.ReportSortSubmissionResult{
		ReportSeq:    vote.ReportSeq,
		Verdict:      normalizedVerdict,
		UrgencyScore: vote.UrgencyScore,
		RewardKITNs:  sortRewardKITNs,
		SortMetrics:  metrics,
	}, nil
}
