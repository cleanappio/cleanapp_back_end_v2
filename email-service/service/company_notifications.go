package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cleanapp-common/httpx"
	"email-service/config"
	"email-service/email"
	"email-service/models"
	"github.com/apex/log"
	"github.com/go-sql-driver/mysql"
)

const companyFreshWindow = 30 * 24 * time.Hour

var companyReaderClient = func() *http.Client {
	client := httpx.NewClient(20 * time.Second)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}()

// A read-only plan contains public report IDs and verified source URLs, never
// inferred/private addresses, credentials or a fabricated successful send.
type CompanyNotificationPlan struct {
	Company        string                 `json:"company"`
	State          string                 `json:"state"`
	Reports        []PlannedCompanyReport `json:"reports"`
	RecipientCount int                    `json:"recipient_count"`
	ContactSources []string               `json:"contact_sources"`
	RetryAt        *time.Time             `json:"retry_at,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	route          CompanyRoute
	total          int
}
type PlannedCompanyReport struct {
	Seq        int64     `json:"seq"`
	PublicID   string    `json:"public_id"`
	Title      string    `json:"title"`
	EvidenceAt time.Time `json:"evidence_at"`

	// Current canonical public projection, never the raw stored evidence. These
	// fields stay out of operator-plan JSON and are refreshed before sending.
	excerpt          string
	impact           string
	hasOriginalMedia bool
}
type companyReaderReport struct {
	Report struct {
		Seq      int64  `json:"seq"`
		PublicID string `json:"public_id"`
	} `json:"report"`
	Analysis []struct {
		Title          string `json:"title"`
		BrandName      string `json:"brand_name"`
		Classification string `json:"classification"`
	} `json:"analysis"`
	Evidence struct {
		Kind             string     `json:"report_kind"`
		Intake           string     `json:"intake_method"`
		Relationship     string     `json:"entity_relationship"`
		Observed         *time.Time `json:"observed_at"`
		Published        *time.Time `json:"source_published_at"`
		Ingested         *time.Time `json:"ingested_at"`
		Excerpt          *string    `json:"excerpt"`
		Impact           *string    `json:"reported_impact"`
		HasOriginalMedia bool       `json:"has_original_media"`
	} `json:"evidence"`
}

func genericCompanyExclusionSQL(column string) string {
	aliases := []string{}
	for _, route := range VerifiedCompanyRoutes() {
		for _, alias := range route.Aliases {
			aliases = append(aliases, "'"+strings.ReplaceAll(alias, "'", "''")+"'")
		}
	}
	// column and aliases are application constants, never request data.
	return "LOWER(TRIM(" + column + ")) NOT IN (" + strings.Join(aliases, ",") + ")"
}

func freshCompanyReport(row companyReaderReport, route CompanyRoute, now time.Time) (PlannedCompanyReport, bool) {
	if row.Report.Seq <= 0 || email.ReportURL(models.BrandReportFinding{PublicID: row.Report.PublicID, Classification: "digital"}) == "" || len(row.Analysis) == 0 || row.Evidence.Kind != "firsthand" {
		return PlannedCompanyReport{}, false
	}
	a := row.Analysis[0]
	owner, known := ResolveCompanyRoute(a.BrandName)
	if !known || owner.CompanyKey != route.CompanyKey || a.Classification != "digital" || row.Evidence.Relationship != "affected" {
		return PlannedCompanyReport{}, false
	}
	date := row.Evidence.Observed
	if date == nil {
		date = row.Evidence.Published
	}
	if date == nil && row.Evidence.Intake == "cleanapp_submission" {
		date = row.Evidence.Ingested
	}
	if date == nil || date.IsZero() || date.Before(now.Add(-companyFreshWindow)) || date.After(now) {
		return PlannedCompanyReport{}, false
	}
	planned := PlannedCompanyReport{Seq: row.Report.Seq, PublicID: row.Report.PublicID, Title: a.Title, EvidenceAt: date.UTC(), hasOriginalMedia: row.Evidence.HasOriginalMedia}
	if row.Evidence.Excerpt != nil {
		planned.excerpt = *row.Evidence.Excerpt
	}
	if row.Evidence.Impact != nil {
		planned.impact = *row.Evidence.Impact
	}
	return planned, true
}

func (s *EmailService) freshCompanyCandidates(ctx context.Context, route CompanyRoute, now time.Time) ([]PlannedCompanyReport, int, error) {
	if s.config.ReportListenerURL == "" {
		return nil, 0, fmt.Errorf("reader URL is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	u, err := url.Parse(strings.TrimRight(s.config.ReportListenerURL, "/") + "/api/v3/reports/by-brand")
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("brand_name", route.CompanyKey)
	q.Set("kind", "firsthand")
	q.Set("sort", "recent")
	q.Set("n", "50")
	q.Set("lang", "en")
	q.Set("full_data", "false")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := companyReaderClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("read canonical company evidence: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("canonical company evidence returned status %d", resp.StatusCode)
	}
	var page struct {
		Reports     []companyReaderReport `json:"reports"`
		Kind        string                `json:"kind"`
		CountsExact bool                  `json:"counts_exact"`
		Scope       struct {
			BrandName string `json:"brand_name"`
		} `json:"scope"`
		Overview struct {
			Total int `json:"total_reports"`
		} `json:"overview"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&page); err != nil {
		return nil, 0, fmt.Errorf("decode company evidence: %w", err)
	}
	if page.Kind != "firsthand" || !page.CountsExact || page.Scope.BrandName != route.CompanyKey || len(page.Reports) > 50 {
		return nil, 0, fmt.Errorf("canonical evidence scope was not confirmed")
	}
	candidates := []PlannedCompanyReport{}
	seen := map[int64]bool{}
	for _, row := range page.Reports {
		r, ok := freshCompanyReport(row, route, now)
		if ok && !seen[r.Seq] {
			seen[r.Seq] = true
			candidates = append(candidates, r)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].EvidenceAt.Equal(candidates[j].EvidenceAt) {
			return candidates[i].Seq > candidates[j].Seq
		}
		return candidates[i].EvidenceAt.After(candidates[j].EvidenceAt)
	})
	return candidates, page.Overview.Total, nil
}

// Verified routing resolves no_valid_emails, and the current short cadence
// replaces obsolete all_throttled holds. Every other future hold,
// accepted receipt, processed marker and current publication restriction wins.
func (s *EmailService) pendingCompanyReports(ctx context.Context, candidates []PlannedCompanyReport) ([]PlannedCompanyReport, error) {
	if len(candidates) == 0 {
		return []PlannedCompanyReport{}, nil
	}
	if len(candidates) > 50 {
		return nil, fmt.Errorf("company candidate limit exceeded")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	marks := make([]string, len(candidates))
	args := make([]any, len(candidates))
	for i, r := range candidates {
		marks[i] = "?"
		args[i] = r.Seq
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.seq FROM reports r JOIN report_analysis ra ON ra.seq=r.seq AND ra.language='en'
		LEFT JOIN report_raw rr ON rr.report_seq=r.seq LEFT JOIN sent_reports_emails sent ON sent.seq=r.seq
 JOIN report_evidence ev ON ev.seq=r.seq AND ev.report_kind='firsthand' AND ev.entity_relationship='affected' AND ev.evidence_version>0
		LEFT JOIN email_report_retry retry ON retry.seq=r.seq
		WHERE r.seq IN (`+strings.Join(marks, ",")+`) AND sent.seq IS NULL AND ra.is_valid=TRUE AND ra.classification='digital'
		AND (rr.visibility IS NULL OR rr.visibility='public') AND `+notificationPublicStateSQL("r")+`
		AND (retry.seq IS NULL OR retry.next_attempt_at<=NOW() OR retry.reason IN ('no_valid_emails','all_throttled'))
		AND (retry.reason IS NULL OR retry.reason<>'notification_receipt_pending')
		AND NOT EXISTS(SELECT 1 FROM report_email_deliveries d WHERE d.seq=r.seq AND d.delivery_status='sent')
		AND NOT EXISTS(SELECT 1 FROM report_email_delivery_events e WHERE e.seq=r.seq AND e.delivery_status='sent')`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := map[int64]bool{}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		allowed[seq] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pending := []PlannedCompanyReport{}
	for _, r := range candidates {
		if allowed[r.Seq] {
			pending = append(pending, r)
			if len(pending) >= email.LinkedReportLimit {
				break
			}
		}
	}
	return pending, nil
}

func routeAliases(route CompanyRoute) []string {
	aliases := append([]string(nil), route.Aliases...)
	for _, key := range []string{route.CompanyKey, route.ThrottleKey} {
		found := false
		for _, alias := range aliases {
			if alias == key {
				found = true
			}
		}
		if !found {
			aliases = append(aliases, key)
		}
	}
	return aliases
}

func (s *EmailService) companyThrottle(ctx context.Context, route CompanyRoute, recipient string, now time.Time) (*time.Time, error) {
	aliases := routeAliases(route)
	marks := make([]string, len(aliases))
	args := []any{strings.ToLower(strings.TrimSpace(recipient))}
	for i, alias := range aliases {
		marks[i] = "?"
		args = append(args, alias)
	}
	var last sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(last_sent_at) FROM brand_email_throttle WHERE LOWER(TRIM(email))=? AND LOWER(TRIM(brand_name)) IN (`+strings.Join(marks, ",")+`)`, args...).Scan(&last); err != nil {
		return nil, err
	}
	period := s.config.GetCompanyNotificationInterval()
	// The append-only acceptance ledger is the durable backstop if the mutable
	// throttle cache failed after SMTP acceptance. Start with indexed brand rows,
	// then join each receipt by seq; never scan the global delivery population.
	receiptArgs := []any{}
	for _, alias := range aliases {
		receiptArgs = append(receiptArgs, alias)
	}
	receiptArgs = append(receiptArgs, strings.ToLower(strings.TrimSpace(recipient)), now.Add(-period))
	receiptArgs = append(receiptArgs, receiptArgs...)
	var accepted sql.NullTime
	query := `SELECT MAX(sent_at) FROM (
 SELECT d.sent_at FROM report_analysis ra STRAIGHT_JOIN report_email_deliveries d FORCE INDEX (uq_report_email_delivery) ON d.seq=ra.seq
 WHERE ra.language='en' AND ra.brand_name IN (` + strings.Join(marks, ",") + `) AND LOWER(TRIM(d.recipient_email))=? AND d.delivery_status='sent' AND d.sent_at>=?
 UNION ALL SELECT d.sent_at FROM report_analysis ra STRAIGHT_JOIN report_email_delivery_events d FORCE INDEX (idx_report_delivery_event_seq) ON d.seq=ra.seq
 WHERE ra.language='en' AND ra.brand_name IN (` + strings.Join(marks, ",") + `) AND LOWER(TRIM(d.recipient_email))=? AND d.delivery_status='sent' AND d.sent_at>=?
 ) accepted`
	if err := s.db.QueryRowContext(ctx, query, receiptArgs...).Scan(&accepted); err != nil {
		return nil, err
	}
	if accepted.Valid && (!last.Valid || accepted.Time.After(last.Time)) {
		last = accepted
	}
	if last.Valid {
		next := last.Time.UTC().Add(period)
		if now.Before(next) {
			return &next, nil
		}
	}

	return nil, nil
}

// Count actual accepted recipient messages, not one throttle row per mailbox or
// one receipt per linked report. ID-less historical batches use their recorded
// recipient/source/provider/time; unknown legacy identity remains conservative.
// A cache timestamp within five seconds of the same recipient's receipt is the
// secondary write for that acceptance. Unmatched cache snapshots still count.
// The five-minute sender cadence keeps distinct future messages well apart.
func (s *EmailService) companyDailyCount(ctx context.Context, route CompanyRoute, now time.Time) (int, error) {
	start, end := companyDailyWindow(now)
	aliases := routeAliases(route)
	marks := make([]string, len(aliases))
	branchArgs := []any{}
	for i, alias := range aliases {
		marks[i] = "?"
		branchArgs = append(branchArgs, alias)
	}
	branchArgs = append(branchArgs, start, end)
	args := append([]any(nil), branchArgs...)
	args = append(args, branchArgs...)
	args = append(args, branchArgs...)
	in := strings.Join(marks, ",")
	query := `WITH company_acceptances AS (
 SELECT LOWER(TRIM(d.recipient_email)) COLLATE utf8mb4_bin AS recipient_email,d.sent_at,
 CASE WHEN NULLIF(TRIM(d.provider_message_id),'') IS NOT NULL THEN
 CONCAT('message:',SHA2(CONCAT_WS(CHAR(0),LOWER(COALESCE(d.provider,'')),LOWER(TRIM(d.recipient_email)),d.provider_message_id),256))
 ELSE CONCAT('legacy:',SHA2(CONCAT_WS(CHAR(0),LOWER(COALESCE(d.provider,'')),LOWER(TRIM(d.recipient_email)),COALESCE(d.delivery_source,''),DATE_FORMAT(d.sent_at,'%Y-%m-%d %H:%i:%s.%f')),256)) END AS message_key
 FROM report_analysis ra STRAIGHT_JOIN report_email_delivery_events d FORCE INDEX (idx_report_delivery_event_seq) ON d.seq=ra.seq
 WHERE ra.language='en' AND ra.brand_name IN (` + in + `) AND d.delivery_status='sent'
 AND REGEXP_LIKE(d.recipient_email,'[^[:space:]]') AND d.sent_at>=? AND d.sent_at<?
 UNION ALL
 SELECT LOWER(TRIM(d.recipient_email)) COLLATE utf8mb4_bin,d.sent_at,
 CONCAT('legacy:',SHA2(CONCAT_WS(CHAR(0),LOWER(COALESCE(d.provider,'')),LOWER(TRIM(d.recipient_email)),COALESCE(d.delivery_source,''),DATE_FORMAT(d.sent_at,'%Y-%m-%d %H:%i:%s.%f')),256))
 FROM report_analysis ra STRAIGHT_JOIN report_email_deliveries d FORCE INDEX (uq_report_email_delivery) ON d.seq=ra.seq
 WHERE ra.language='en' AND ra.brand_name IN (` + in + `) AND d.delivery_status='sent'
 AND REGEXP_LIKE(d.recipient_email,'[^[:space:]]') AND d.sent_at>=? AND d.sent_at<?
 AND NOT EXISTS(SELECT 1 FROM report_email_delivery_events e FORCE INDEX (idx_report_delivery_event_seq)
 WHERE e.seq=d.seq AND LOWER(TRIM(e.recipient_email)) COLLATE utf8mb4_bin=LOWER(TRIM(d.recipient_email)) COLLATE utf8mb4_bin AND e.delivery_source COLLATE utf8mb4_bin=d.delivery_source COLLATE utf8mb4_bin AND e.sent_at<=>d.sent_at AND e.delivery_status='sent')
 )
 SELECT COUNT(DISTINCT message_key) FROM (
 SELECT message_key FROM company_acceptances
 UNION ALL
 SELECT CONCAT('cache:',SHA2(CONCAT_WS(CHAR(0),LOWER(TRIM(c.email)),DATE_FORMAT(c.last_sent_at,'%Y-%m-%d %H:%i:%s.%f')),256))
 FROM brand_email_throttle c
 WHERE LOWER(TRIM(c.brand_name)) IN (` + in + `) AND REGEXP_LIKE(c.email,'[^[:space:]]') AND c.last_sent_at>=? AND c.last_sent_at<?
 AND NOT EXISTS(SELECT 1 FROM company_acceptances a WHERE a.recipient_email=LOWER(TRIM(c.email)) COLLATE utf8mb4_bin
 AND a.sent_at BETWEEN DATE_SUB(c.last_sent_at,INTERVAL 5 SECOND) AND DATE_ADD(c.last_sent_at,INTERVAL 5 SECOND))
 ) accepted_messages`
	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func companyDailyWindow(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 0, 1)
}

func (s *EmailService) buildCompanyPlan(ctx context.Context, route CompanyRoute, now time.Time) (CompanyNotificationPlan, error) {
	plan := CompanyNotificationPlan{Company: route.CompanyKey, State: "unavailable", Reports: []PlannedCompanyReport{}, ContactSources: []string{}, route: route}
	for _, r := range route.Recipients {
		plan.ContactSources = append(plan.ContactSources, r.SourceURL)
	}
	if len(route.Recipients) == 0 {
		return plan, fmt.Errorf("verified company route has no recipient")
	}
	recipient := route.Recipients[0].Email
	plan.RecipientCount = 1
	policyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	optedOut, err := s.isEmailOptedOut(policyCtx, recipient)
	if err != nil {
		return plan, err
	}
	if optedOut {
		plan.State = "opted_out"
		return plan, nil
	}
	next, err := s.companyThrottle(policyCtx, route, recipient, now)
	if err != nil {
		return plan, err
	}
	if next != nil {
		plan.State = "throttled"
		plan.RetryAt = next
		return plan, nil
	}
	daily, err := s.companyDailyCount(policyCtx, route, now)
	if err != nil {
		return plan, err
	}
	max := s.config.MaxDailyEmailsPerBrand
	if max <= 0 {
		max = 10
	}
	if daily >= max {
		plan.State = "daily_limit"
		_, retry := companyDailyWindow(now)
		plan.RetryAt = &retry
		return plan, nil
	}
	// A recipient hold is authoritative without discovering reports. Once it
	// clears, use the original context so the reader retains its own deadline.
	plan.State = "no_recent_reports"
	candidates, total, err := s.freshCompanyCandidates(ctx, route, now)
	if err != nil {
		return plan, err
	}
	plan.total = total
	if len(candidates) == 0 {
		return plan, nil
	}
	plan.Reports, err = s.pendingCompanyReports(ctx, candidates)
	if err != nil {
		return plan, err
	}
	if len(plan.Reports) == 0 {
		plan.State = "no_pending_reports"
		return plan, nil
	}
	plan.State = "ready"
	return plan, nil
}

// NewNotificationPlanService deliberately constructs no email sender and performs
// no schema setup. Its only startup database operation is a bounded ping.
func NewNotificationPlanService(ctx context.Context, cfg *config.Config) (*EmailService, error) {
	dbc := mysql.NewConfig()
	dbc.User = cfg.DBUser
	dbc.Passwd = cfg.DBPassword
	dbc.Net = "tcp"
	dbc.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	dbc.DBName = cfg.DBName
	dbc.ParseTime = true
	dbc.Params = map[string]string{"time_zone": "'+00:00'"}
	dbc.Timeout = 5 * time.Second
	dbc.ReadTimeout = 5 * time.Second
	dbc.WriteTimeout = 5 * time.Second
	db, err := sql.Open("mysql", dbc.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open notification plan database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping notification plan database: %w", err)
	}
	return &EmailService{db: db, config: cfg}, nil
}

// BuildCompanyNotificationPlans is always read-only, even when production
// EMAIL_DRY_RUN is false. It is safe for reviewing a bounded recovery batch.
func (s *EmailService) BuildCompanyNotificationPlans(ctx context.Context, now time.Time) []CompanyNotificationPlan {
	plans := []CompanyNotificationPlan{}
	for _, route := range VerifiedCompanyRoutes() {
		plan, err := s.buildCompanyPlan(ctx, route, now)
		if err != nil {
			plan.State = "unavailable"
			plan.Reason = err.Error()
		}
		plans = append(plans, plan)
	}
	return plans
}

// ProcessCompanyNotifications runs independently from the slow legacy global
// selector; known AI aliases are explicitly excluded from that legacy path.
func (s *EmailService) ProcessCompanyNotifications() error {
	ctx := context.Background()
	work := func(route CompanyRoute) error {
		plan, err := s.buildCompanyPlan(ctx, route, time.Now().UTC())
		if err != nil {
			return err
		}
		return s.processCompanyPlan(ctx, plan)
	}
	if s.config.DryRun {
		return processCompanyRoutes(VerifiedCompanyRoutes(), work)
	}
	return processCompanyRoutes(VerifiedCompanyRoutes(), func(route CompanyRoute) error {
		ran, err := s.withCompanyNotificationLock(ctx, route.CompanyKey, func() error { return work(route) })
		if err == nil && !ran {
			log.Infof("Company notification worker already active: company=%s", route.CompanyKey)
		}
		return err
	})
}

func processCompanyRoutes(routes []CompanyRoute, work func(CompanyRoute) error) error {
	failures := []error{}
	for _, route := range routes {
		if err := work(route); err != nil {
			log.Warnf("Company notification cycle failed: company=%s error=%v", route.CompanyKey, err)
			failures = append(failures, fmt.Errorf("company %s: %w", route.CompanyKey, err))
		}
	}
	return errors.Join(failures...)
}

func (s *EmailService) processCompanyPlan(ctx context.Context, plan CompanyNotificationPlan) error {
	log.Infof("Company notification plan: company=%s state=%s reports=%d recipients=%d", plan.Company, plan.State, len(plan.Reports), plan.RecipientCount)
	if s.config.DryRun {
		return nil
	}
	if plan.RetryAt != nil {
		// Policy-first held plans deliberately contain no discovered report
		// IDs. Do not write retry rows for a batch that was never selected.
		if len(plan.Reports) == 0 {
			return nil
		}
		seqs := make([]int64, len(plan.Reports))
		for i, r := range plan.Reports {
			seqs[i] = r.Seq
		}
		reason := retryReasonAllThrottled
		if plan.State == "daily_limit" {
			reason = retryReasonDailyLimit
		}
		s.scheduleReportsRetry(ctx, seqs, reason, *plan.RetryAt)
		return nil
	}
	if plan.State != "ready" {
		return nil
	}
	if len(plan.route.Recipients) == 0 {
		return fmt.Errorf("verified company route has no recipient")
	}
	if err := s.preflightNotificationReceipts(ctx); err != nil {
		return fmt.Errorf("company receipt preflight: %w", err)
	}
	// Hydrate only selected findings, including a current public/validity check.
	seqs := make([]int64, len(plan.Reports))
	for i, r := range plan.Reports {
		seqs[i] = r.Seq
	}
	summary := models.BrandReportSummary{BrandName: plan.Company, BrandDisplayName: plan.route.DisplayName, Classification: "digital", TotalReportCount: plan.total, ReportSeqs: seqs}
	findings, err := s.getAggregateFindingsForBrands(ctx, &summary, routeAliases(plan.route))
	if err != nil {
		return err
	}
	bySeq := map[int64]models.BrandReportFinding{}
	for _, r := range findings {
		if r.ReportKind == "firsthand" && r.Classification == "digital" {
			bySeq[r.Seq] = r
		}
	}
	// Re-evaluate freshness, processed/accepted state, private/status changes and
	// retry holds after image/reader work, immediately before recipient policy.
	now := time.Now().UTC()
	current, _, err := s.freshCompanyCandidates(ctx, plan.route, now)
	if err != nil {
		return err
	}
	planned := map[int64]bool{}
	for _, r := range plan.Reports {
		planned[r.Seq] = true
	}
	fresh := []PlannedCompanyReport{}
	for _, r := range current {
		if planned[r.Seq] {
			if finding, ok := bySeq[r.Seq]; ok && finding.PublicID == r.PublicID {
				// The canonical reader applies Wire privacy flags and sensitive
				// text filtering. Missing projected text must clear raw values;
				// image eligibility must also come from this current response.
				finding.OriginalExcerpt = r.excerpt
				finding.ReportedImpact = r.impact
				if !r.hasOriginalMedia {
					finding.Image = nil
				}
				bySeq[r.Seq] = finding
				fresh = append(fresh, r)
			}
		}
	}
	pending, err := s.pendingCompanyReports(ctx, fresh)
	if err != nil {
		return err
	}
	summary.ReportSeqs = nil
	summary.Reports = nil
	for _, r := range pending {
		summary.ReportSeqs = append(summary.ReportSeqs, r.Seq)
		summary.Reports = append(summary.Reports, bySeq[r.Seq])
	}
	if len(summary.Reports) == 0 {
		return nil
	}
	summary.NewReportCount = len(summary.Reports)
	recipient := plan.route.Recipients[0].Email
	now = time.Now().UTC()
	policyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	optedOut, err := s.isEmailOptedOut(policyCtx, recipient)
	if err != nil {
		return err
	}
	if optedOut {
		return nil
	}
	next, err := s.companyThrottle(policyCtx, plan.route, recipient, now)
	if err != nil {
		return err
	}
	if next != nil {
		return nil
	}
	daily, err := s.companyDailyCount(policyCtx, plan.route, now)
	if err != nil {
		return err
	}
	max := s.config.MaxDailyEmailsPerBrand
	if max <= 0 {
		max = 10
	}
	if daily >= max {
		return nil
	}
	results := s.email.SendAggregateEmailsWithResults([]string{recipient}, &summary, s.config.OptOutURL)
	if len(results) != 1 {
		return fmt.Errorf("company sender returned no per-recipient result")
	}
	result := results[0]
	if result.Status != "sent" {
		s.scheduleReportsRetry(ctx, summary.ReportSeqs, retryReasonAggregateFailure, time.Now().Add(30*time.Minute))
		log.Warnf("Company notification rejected: company=%s error=%s", plan.Company, result.Error)
		return nil
	}
	if err := s.recordBrandEmailSent(ctx, plan.route.ThrottleKey, recipient); err != nil {
		log.Warnf("Company notification throttle persistence failed: %v", err)
	}
	if err := s.recordEmailSent(ctx, recipient); err != nil {
		log.Warnf("Company recipient history persistence failed: %v", err)
	}
	var receiptErr error
	for _, seq := range summary.ReportSeqs {
		if receiptErr == nil {
			receiptErr = s.recordReportEmailDeliveryResult(ctx, seq, recipient, "aggregate_report", result)
		}
		if receiptErr != nil {
			recovery, _ := json.Marshal(map[string]any{"seq": seq, "recipient": recipient, "source": "aggregate_report", "provider": result.Provider, "provider_message_id": result.ProviderMessageID, "sent_at": result.SentAt, "error": receiptErr.Error()})
			log.Errorf("notification_receipt_recovery %s", recovery)
		}
	}
	if receiptErr != nil {
		s.scheduleReportsRetry(ctx, summary.ReportSeqs, retryReasonReceiptPending, time.Now().Add(24*time.Hour))
		return fmt.Errorf("accepted company notification receipt failed; batch quarantined: %w", receiptErr)
	}
	for _, seq := range summary.ReportSeqs {
		if err := s.markReportAsProcessed(ctx, seq); err != nil {
			log.Warnf("Company report processed marker failed: %v", err)
		}
	}
	log.Infof("Company notification accepted: company=%s reports=%d emails=1", plan.Company, len(summary.ReportSeqs))
	return nil
}
