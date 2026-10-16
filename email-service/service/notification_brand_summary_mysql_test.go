package service

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"email-service/models"
)

// legacyBrandSummaryQuery is the pre-change query, kept verbatim as the parity
// oracle. It evaluated the correlated total for every pending brand.
func legacyBrandSummaryQuery() string {
	return `
		SELECT 
			ra.brand_name,
			COALESCE(ra.brand_display_name, ra.brand_name) as brand_display_name,
			COUNT(*) as new_count,
			(SELECT COUNT(*) FROM report_analysis ra2
			  LEFT JOIN report_raw rr2 ON ra2.seq = rr2.report_seq
			  WHERE ra2.brand_name = ra.brand_name AND ra2.language = 'en' AND ra2.is_valid = TRUE
			    AND (rr2.visibility IS NULL OR rr2.visibility = 'public')
			    AND ` + notificationPublicStateSQL("ra2") + `
			) as total_count,
			GROUP_CONCAT(CONCAT(r.seq, ':') ORDER BY r.seq DESC) as seqs,
			MAX(r.seq) as latest_seq,
			MAX(ra.classification) as classification,
			MAX(ra.inferred_contact_emails) as inferred_contact_emails
		FROM reports r
		INNER JOIN report_analysis ra ON r.seq = ra.seq
		LEFT JOIN report_raw rr ON r.seq = rr.report_seq
		LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
		LEFT JOIN email_report_retry erry ON r.seq = erry.seq
		WHERE sre.seq IS NULL
		  AND ra.brand_name != ''
		  AND ra.is_valid = TRUE
		  AND ra.language = 'en'
		  AND (rr.visibility IS NULL OR rr.visibility = 'public')
		  AND ` + notificationPublicStateSQL("r") + `
		  AND (erry.seq IS NULL OR erry.next_attempt_at <= NOW())
		AND (erry.reason IS NULL OR erry.reason <> 'notification_receipt_pending')
		AND ` + genericCompanyExclusionSQL("ra.brand_name") + `
		GROUP BY ra.brand_name, ra.brand_display_name
		ORDER BY new_count DESC
		LIMIT 100
	`
}

func TestBrandSummaryCountsTotalsOnlyForSelectedBrands(t *testing.T) {
	query := brandSummaryQuery()
	derived := strings.Index(query, "FROM (")
	total := strings.Index(query, "ra2.brand_name = s.brand_name")
	limit := strings.Index(query, "LIMIT 100")
	closed := strings.Index(query, ") s")
	if derived < 0 || total < 0 || total > derived || limit < derived || limit > closed || strings.Count(query, "LIMIT 100") != 1 || strings.Contains(query, "ra2.brand_name = ra.brand_name") {
		t.Fatal("brand total must be evaluated outside the top-100 pending selection")
	}
}

func readBrandSummaries(ctx context.Context, t *testing.T, db *sql.DB, query string) []models.BrandReportSummary {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []models.BrandReportSummary
	for rows.Next() {
		var s models.BrandReportSummary
		var seqs string
		var emails sql.NullString
		if err := rows.Scan(&s.BrandName, &s.BrandDisplayName, &s.NewReportCount, &s.TotalReportCount, &seqs, &s.LatestReportSeq, &s.Classification, &emails); err != nil {
			t.Fatal(err)
		}
		s.ReportSeqs = parseCompleteReportSeqs(seqs)
		s.InferredContactEmails = emails.String
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// 101 pending brands where only brand 1 ranks below the others make the
// top-100 cutoff deterministic, so the rewritten query must select the same
// brands with the same totals as the legacy oracle. Totals include sent and
// exclude private, inactive and shadow reports. The fixture stays small
// because the legacy query rescans every pending brand's history.
func testBrandSummaryLegacyParityMySQL(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	const base, brands = 200000, 101
	t.Cleanup(func() {
		for _, table := range []string{"reports", "report_analysis", "sent_reports_emails", "report_status", "email_report_retry"} {
			db.Exec("DELETE FROM "+table+" WHERE seq BETWEEN ? AND ?", base, base+brands*1000+999)
		}
		db.Exec("DELETE FROM reports_owners WHERE seq BETWEEN ? AND ?", base, base+brands*1000+999)
		db.Exec("DELETE FROM report_raw WHERE report_seq BETWEEN ? AND ?", base, base+brands*1000+999)
	})
	for b := 1; b <= brands; b++ {
		values, args := []string{}, []any{}
		analysis, analysisArgs := []string{}, []any{}
		count := 4
		switch b {
		case 1:
			count = 2
		case 50, 60, 70, 80:
			count = 5
		}
		for i := 1; i <= count; i++ {
			seq := base + b*1000 + i
			values = append(values, "(?,?,'2026-10-15',NULL)")
			args = append(args, seq, fmt.Sprintf("rpt_parity_%d", seq))
			analysis = append(analysis, "(?,'en',?,?,'Issue','Summary','digital','support@company.com',TRUE)")
			analysisArgs = append(analysisArgs, seq, fmt.Sprintf("parity-brand-%03d", b), fmt.Sprintf("Parity %03d", b))
		}
		exec("INSERT INTO reports(seq,public_id,ts,image) VALUES"+strings.Join(values, ","), args...)
		exec("INSERT INTO report_analysis VALUES"+strings.Join(analysis, ","), analysisArgs...)
		// One already-sent report per brand counts toward total but not pending.
		exec("INSERT INTO sent_reports_emails VALUES(?)", base+b*1000+1)
	}
	// Publication guards on both scopes: private owner, inactive status and
	// shadow visibility leave pending and total; a deferred retry leaves pending only.
	exec("INSERT INTO reports_owners VALUES(?,'private-owner',FALSE)", base+50*1000+2)
	exec("INSERT INTO report_status VALUES(?,'resolved')", base+60*1000+2)
	exec("INSERT INTO report_raw VALUES(?,'shadow')", base+70*1000+2)
	exec("INSERT INTO email_report_retry VALUES(?,DATE_ADD(NOW(),INTERVAL 1 DAY),'no_valid_emails')", base+80*1000+2)

	legacy := readBrandSummaries(ctx, t, db, legacyBrandSummaryQuery())
	got := readBrandSummaries(ctx, t, db, brandSummaryQuery())
	for i := 1; i < len(got); i++ {
		if got[i].NewReportCount > got[i-1].NewReportCount {
			t.Fatal("brand summary is not ordered by pending count")
		}
	}
	// Equal pending counts have no defined order in either query.
	byName := func(s []models.BrandReportSummary) []models.BrandReportSummary {
		s = append([]models.BrandReportSummary(nil), s...)
		sort.Slice(s, func(i, j int) bool { return s[i].BrandName < s[j].BrandName })
		return s
	}
	if len(legacy) != 100 || !reflect.DeepEqual(byName(got), byName(legacy)) {
		t.Fatalf("brand summary diverged from legacy oracle: got %d rows, legacy %d", len(got), len(legacy))
	}
	byBrand := map[string]models.BrandReportSummary{}
	for _, s := range got {
		byBrand[s.BrandName] = s
	}
	for b, want := range map[int][2]int{2: {3, 4}, 50: {3, 4}, 60: {3, 4}, 70: {3, 4}, 80: {3, 5}, 101: {3, 4}} {
		s := byBrand[fmt.Sprintf("parity-brand-%03d", b)]
		if s.NewReportCount != want[0] || s.TotalReportCount != want[1] {
			t.Fatalf("brand %d counts = %d/%d, want %d/%d", b, s.NewReportCount, s.TotalReportCount, want[0], want[1])
		}
	}
	if _, ok := byBrand["parity-brand-001"]; ok {
		t.Fatal("brand below the top-100 cutoff was selected")
	}
}
