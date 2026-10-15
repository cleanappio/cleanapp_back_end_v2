package service

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"email-service/email"
	"email-service/models"
)

func parseCompleteReportSeqs(value string) []int64 {
	var seqs []int64
	seen := make(map[int64]bool)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasSuffix(part, ":") {
			continue
		}
		seq, err := strconv.ParseInt(strings.TrimSuffix(part, ":"), 10, 64)
		if err == nil && seq > 0 && !seen[seq] {
			seqs = append(seqs, seq)
			seen[seq] = true
		}
	}
	return seqs
}

// Match the canonical public report policy, including legacy owner privacy and
// inactive reports. Aliases are fixed at call sites, never supplied by callers.
func notificationPublicStateSQL(alias string) string {
	return `NOT EXISTS (SELECT 1 FROM report_status ns WHERE ns.seq=` + alias + `.seq AND ns.status IS NOT NULL AND ns.status<>'active')
		AND NOT EXISTS (SELECT 1 FROM reports_owners nowner WHERE nowner.seq=` + alias + `.seq AND nowner.owner IS NOT NULL AND nowner.owner<>'' AND nowner.is_public=FALSE)`
}

// getAggregateFindings only considers the current pending batch. It cannot
// select historical, already processed reports merely because they rank highly.
func (s *EmailService) getAggregateFindings(ctx context.Context, summary *models.BrandReportSummary) ([]models.BrandReportFinding, error) {
	return s.getAggregateFindingsForBrands(ctx, summary, []string{summary.BrandName})
}

func (s *EmailService) getAggregateFindingsForBrands(ctx context.Context, summary *models.BrandReportSummary, brands []string) ([]models.BrandReportFinding, error) {
	if len(summary.ReportSeqs) == 0 || len(brands) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	marks := make([]string, len(summary.ReportSeqs))
	args := make([]any, 0, len(summary.ReportSeqs)+1)
	for i, seq := range summary.ReportSeqs {
		marks[i] = "?"
		args = append(args, seq)
	}
	brandMarks := make([]string, len(brands))
	for i, brand := range brands {
		brandMarks[i] = "?"
		args = append(args, brand)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.seq,r.public_id,COALESCE(ra.title,''),COALESCE(ra.summary,''),
		COALESCE(ev.evidence_excerpt,''),COALESCE(ev.reported_impact,''),COALESCE(ev.report_kind,'unknown'),
		COALESCE(ra.classification,''),r.ts
		FROM reports r JOIN report_analysis ra ON ra.seq=r.seq AND ra.language='en'
		LEFT JOIN report_raw rr ON rr.report_seq=r.seq LEFT JOIN report_evidence ev ON ev.seq=r.seq
		WHERE r.seq IN (`+strings.Join(marks, ",")+`) AND ra.brand_name IN (`+strings.Join(brandMarks, ",")+`) AND ra.is_valid=TRUE
		AND (rr.visibility IS NULL OR rr.visibility='public') AND `+notificationPublicStateSQL("r")+` AND r.public_id IS NOT NULL AND r.public_id<>''
		ORDER BY r.ts DESC,r.seq DESC LIMIT 20`, args...)
	if err != nil {
		return nil, fmt.Errorf("load pending report findings: %w", err)
	}
	findings := make([]models.BrandReportFinding, 0, email.LinkedReportLimit)
	for rows.Next() {
		var r models.BrandReportFinding
		var received sql.NullTime
		if err := rows.Scan(&r.Seq, &r.PublicID, &r.Title, &r.Summary, &r.OriginalExcerpt, &r.ReportedImpact, &r.ReportKind, &r.Classification, &received); err != nil {
			rows.Close()
			return nil, err
		}
		if received.Valid {
			r.ReceivedAt = received.Time
		}
		if email.ReportURL(r) != "" {
			findings = append(findings, r)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := 0; i < len(findings) && i < email.FeaturedReportLimit; i++ {
		var image []byte
		// Avoid transferring unbounded original blobs; only email-sized previews
		// are decoded. The direct link still opens the full original image.
		if err := s.db.QueryRowContext(ctx, `SELECT CASE WHEN OCTET_LENGTH(r.image)<=? THEN r.image ELSE NULL END FROM reports r
			LEFT JOIN report_raw rr ON rr.report_seq=r.seq WHERE r.seq=? AND (rr.visibility IS NULL OR rr.visibility='public')
			AND `+notificationPublicStateSQL("r"), email.MaxPreviewInputBytes, findings[i].Seq).Scan(&image); err != nil {
			return nil, fmt.Errorf("load report preview: %w", err)
		}
		findings[i].Image = email.PrepareReportPreview(image)
	}
	return findings, nil
}

// alreadyNotifiedAllReports prevents a successful recipient from being resent
// the same pending batch when another recipient failed and is being retried.
func (s *EmailService) alreadyNotifiedAllReports(ctx context.Context, seqs []int64, recipient string) (bool, error) {
	if len(seqs) == 0 {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	marks := make([]string, len(seqs))
	args := []any{strings.ToLower(strings.TrimSpace(recipient))}
	for i, seq := range seqs {
		marks[i] = "?"
		args = append(args, seq)
	}
	var count int
	query := `SELECT COUNT(DISTINCT seq) FROM (
 SELECT seq FROM report_email_deliveries WHERE recipient_email=? AND delivery_status='sent' AND seq IN (` + strings.Join(marks, ",") + `)
 UNION ALL SELECT seq FROM report_email_delivery_events WHERE recipient_email=? AND delivery_status='sent' AND seq IN (` + strings.Join(marks, ",") + `)
 ) accepted`
	args = append(args, args...)
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count == len(seqs), err
}

func aggregateDeliverySource(seq int64, reports []models.BrandReportFinding) string {
	for i, r := range reports {
		if i >= email.LinkedReportLimit {
			break
		}
		if r.Seq == seq && email.ReportURL(r) != "" {
			return "aggregate_report"
		}
	}
	return "aggregate_brand"
}
