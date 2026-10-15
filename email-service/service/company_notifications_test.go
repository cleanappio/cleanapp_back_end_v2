package service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"email-service/config"
	"email-service/email"
	"email-service/models"
	"github.com/DATA-DOG/go-sqlmock"
)

func companyFixture(seq int64, at time.Time) companyReaderReport {
	var row companyReaderReport
	row.Report.Seq, row.Report.PublicID = seq, "rpt_safe"
	_ = json.Unmarshal([]byte(`[{"title":"A reported failure","brand_name":"grok","classification":"digital"}]`), &row.Analysis)
	row.Evidence.Kind, row.Evidence.Relationship = "firsthand", "affected"
	row.Evidence.Intake, row.Evidence.Published = "public_import", &at
	return row
}

func companyFixtureServer(t *testing.T, company string, reports func() []companyReaderReport) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v3/reports/by-brand" || q.Get("brand_name") != company || q.Get("kind") != "firsthand" || q.Get("sort") != "recent" || q.Get("n") != "50" || q.Get("full_data") != "false" {
			t.Errorf("unbounded or incorrect reader request: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"reports": reports(), "kind": "firsthand", "counts_exact": true, "scope": map[string]any{"brand_name": company}, "overview": map[string]any{"total_reports": 301}})
	}))
}

func TestCompanyFreshnessUsesOriginalDatesAndRequiresAffectedFirsthand(t *testing.T) {
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	route, _ := ResolveCompanyRoute("grok")
	old, future := now.Add(-31*24*time.Hour), now.Add(time.Second)
	cases := []struct {
		name string
		edit func(*companyReaderReport)
		want bool
	}{
		{"recent original", func(*companyReaderReport) {}, true},
		{"coverage", func(r *companyReaderReport) { r.Evidence.Kind = "coverage" }, false},
		{"dependency", func(r *companyReaderReport) { r.Evidence.Relationship = "dependency" }, false},
		{"mention", func(r *companyReaderReport) { r.Evidence.Relationship = "mentioned" }, false},
		{"old import just ingested", func(r *companyReaderReport) { r.Evidence.Published = &old; r.Evidence.Ingested = &now }, false},
		{"unknown imported date", func(r *companyReaderReport) { r.Evidence.Published = nil; r.Evidence.Ingested = &now }, false},
		{"native receipt date", func(r *companyReaderReport) {
			r.Evidence.Intake = "cleanapp_submission"
			r.Evidence.Published = nil
			r.Evidence.Ingested = &now
		}, true},
		{"native known old source", func(r *companyReaderReport) {
			r.Evidence.Intake = "cleanapp_submission"
			r.Evidence.Published = &old
			r.Evidence.Ingested = &now
		}, false},
		{"observed date wins", func(r *companyReaderReport) { r.Evidence.Observed = &old }, false},
		{"future source", func(r *companyReaderReport) { r.Evidence.Published = &future }, false},
		{"wrong company", func(r *companyReaderReport) { r.Analysis[0].BrandName = "twitter" }, false},
		{"invalid link", func(r *companyReaderReport) { r.Report.PublicID = "../private" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := companyFixture(1, now.Add(-time.Hour))
			tc.edit(&r)
			if _, got := freshCompanyReport(r, route, now); got != tc.want {
				t.Fatalf("eligible=%v want %v", got, tc.want)
			}
		})
	}
}

func TestCompanyCandidatesBoundedNewestDeduplicatedAndScopeConfirmed(t *testing.T) {
	now := time.Now().UTC()
	route, _ := ResolveCompanyRoute("grok")
	server := companyFixtureServer(t, "xai", func() []companyReaderReport {
		return []companyReaderReport{companyFixture(1, now.Add(-time.Hour)), companyFixture(2, now.Add(-time.Minute)), companyFixture(2, now.Add(-time.Minute)), companyFixture(3, now.Add(-31*24*time.Hour))}
	})
	defer server.Close()
	s := &EmailService{config: &config.Config{ReportListenerURL: server.URL}}
	got, total, err := s.freshCompanyCandidates(context.Background(), route, now)
	if err != nil || total != 301 || len(got) != 2 || got[0].Seq != 2 || got[1].Seq != 1 {
		t.Fatalf("wrong bounded candidates: %#v %d %v", got, total, err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"firsthand","counts_exact":true,"scope":{"brand_name":"openai"}}`))
	}))
	defer bad.Close()
	s.config.ReportListenerURL = bad.URL
	if _, _, err := s.freshCompanyCandidates(context.Background(), route, now); err == nil {
		t.Fatal("unconfirmed company scope accepted")
	}
}

func companySQLArgs(route CompanyRoute, prefix ...driver.Value) []driver.Value {
	args := append([]driver.Value(nil), prefix...)
	for _, alias := range routeAliases(route) {
		args = append(args, alias)
	}
	return args
}

const companyDailyQueryPattern = `(?s)WITH company_acceptances AS.*COUNT\(DISTINCT message_key\).*accepted_messages`

func expectCompanyPolicyAllowed(mock sqlmock.Sqlmock, route CompanyRoute) {
	recipient := route.Recipients[0].Email
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs(recipient).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\) FROM brand_email_throttle`).WithArgs(companySQLArgs(route, recipient)...).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(nil))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	mock.ExpectQuery(companyDailyQueryPattern).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

func TestCompanyPlanChecksRecipientPolicyBeforeReaderAndMakesNoHeldBatchWrites(t *testing.T) {
	for _, scenario := range []string{"opted_out", "throttled", "daily_limit", "optout_error", "cache_error", "ledger_error", "daily_error"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			route, _ := ResolveCompanyRoute("grok")
			recipient := route.Recipients[0].Email
			now := time.Now().UTC().Truncate(time.Second)
			last := now.Add(-time.Minute)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "reader must not be called", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			optout := mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs(recipient)
			if scenario == "optout_error" {
				optout.WillReturnError(errors.New("policy unavailable"))
			} else if scenario == "opted_out" {
				optout.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			} else {
				optout.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
				cache := mock.ExpectQuery(`SELECT MAX\(last_sent_at\) FROM brand_email_throttle`).WithArgs(companySQLArgs(route, recipient)...)
				if scenario == "cache_error" {
					cache.WillReturnError(errors.New("cache unavailable"))
				} else {
					if scenario == "throttled" {
						cache.WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(last))
					} else {
						cache.WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(nil))
					}
					ledger := mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`)
					if scenario == "ledger_error" {
						ledger.WillReturnError(errors.New("ledger unavailable"))
					} else {
						ledger.WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
						if scenario != "throttled" {
							daily := mock.ExpectQuery(companyDailyQueryPattern)
							if scenario == "daily_error" {
								daily.WillReturnError(errors.New("budget unavailable"))
							} else {
								daily.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
							}
						}
					}
				}
			}
			sender := &aggregateTestSender{}
			s := &EmailService{db: db, email: sender, config: &config.Config{ReportListenerURL: server.URL, MaxDailyEmailsPerBrand: 10}}
			plan, err := s.buildCompanyPlan(context.Background(), route, now)
			if strings.HasSuffix(scenario, "_error") {
				if err == nil || plan.State != "unavailable" {
					t.Fatalf("policy failure was not closed: %#v %v", plan, err)
				}
			} else {
				if err != nil || plan.State != scenario {
					t.Fatalf("wrong policy state: %#v %v", plan, err)
				}
				if scenario == "throttled" && (plan.RetryAt == nil || !plan.RetryAt.Equal(last.Add(5*time.Minute))) {
					t.Fatalf("cooldown lost: %#v", plan)
				}
				if scenario == "daily_limit" && plan.RetryAt == nil {
					t.Fatal("daily retry time lost")
				}
				if err := s.processCompanyPlan(context.Background(), plan); err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 0 || len(sender.recipients) != 0 || len(plan.Reports) != 0 || plan.RecipientCount != 1 || len(plan.ContactSources) != 1 {
				t.Fatalf("held plan fetched/sent/invented reports: %#v calls=%d", plan, calls.Load())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEligibleCompanyPlanStillLoadsOnlyFreshCurrentPendingReports(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	route, _ := ResolveCompanyRoute("grok")
	now := time.Now().UTC()
	expectCompanyPolicyAllowed(mock, route)
	var calls atomic.Int32
	server := companyFixtureServer(t, "xai", func() []companyReaderReport {
		calls.Add(1)
		// All policy reads must finish before the first reader request.
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("reader called before policy completed: %v", err)
		}
		expectCompanyPending(mock, []int64{10}, []int64{10})
		coverage := companyFixture(12, now.Add(-time.Hour))
		coverage.Evidence.Kind = "coverage"
		return []companyReaderReport{companyFixture(10, now.Add(-time.Hour)), companyFixture(11, now.Add(-31*24*time.Hour)), coverage}
	})
	defer server.Close()
	s := &EmailService{db: db, config: &config.Config{ReportListenerURL: server.URL, MaxDailyEmailsPerBrand: 10}}
	plan, err := s.buildCompanyPlan(context.Background(), route, now)
	if err != nil || plan.State != "ready" || calls.Load() != 1 || len(plan.Reports) != 1 || plan.Reports[0].Seq != 10 || plan.total != 301 {
		t.Fatalf("eligible fresh report planning regressed: %#v calls=%d err=%v", plan, calls.Load(), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyThrottleRetainsOfficialRecipientAndAllAliasHistory(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	route, _ := ResolveCompanyRoute("claude-code")
	now := time.Now().UTC().Truncate(time.Second)
	previous := now.Add(-time.Minute)
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\) FROM brand_email_throttle WHERE LOWER\(TRIM\(email\)\)=\? AND LOWER\(TRIM\(brand_name\)\) IN`).WithArgs(companySQLArgs(route, "support@anthropic.com")...).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(previous))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*STRAIGHT_JOIN report_email_deliveries.*UNION ALL.*STRAIGHT_JOIN report_email_delivery_events`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	next, err := (&EmailService{db: db, config: &config.Config{ThrottleDays: 7}}).companyThrottle(context.Background(), route, " Support@Anthropic.com ", now)
	if err != nil || next == nil || !next.Equal(previous.Add(5*time.Minute)) {
		t.Fatalf("alias cooldown bypassed: %v %v", next, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyThrottleUsesDurableAcceptanceWhenCacheWasNotWritten(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	route, _ := ResolveCompanyRoute("grok")
	now := time.Now().UTC().Truncate(time.Second)
	at := now.Add(-time.Minute)
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\) FROM brand_email_throttle`).WithArgs(companySQLArgs(route, "support@x.ai")...).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(nil))
	args := companySQLArgs(route)
	args = append(args, "support@x.ai", now.Add(-5*time.Minute))
	args = append(args, args...)
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*report_analysis ra STRAIGHT_JOIN report_email_deliveries.*UNION ALL.*report_analysis ra STRAIGHT_JOIN report_email_delivery_events`).WithArgs(args...).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(at))
	next, err := (&EmailService{db: db, config: &config.Config{ThrottleDays: 7}}).companyThrottle(context.Background(), route, "support@x.ai", now)
	if err != nil || next == nil || !next.Equal(at.Add(5*time.Minute)) {
		t.Fatalf("accepted send lost cooldown after throttle-cache failure: %v %v", next, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyThrottleUsesNewerAcceptanceThanStaleCache(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	route, _ := ResolveCompanyRoute("grok")
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	cache := now.Add(-3 * time.Minute)
	accepted := now.Add(-time.Minute)
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\)`).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(cache))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(accepted))
	next, err := (&EmailService{db: db, config: &config.Config{ThrottleDays: 7}}).companyThrottle(context.Background(), route, "support@x.ai", now)
	if err != nil || next == nil || !next.Equal(accepted.Add(5*time.Minute)) {
		t.Fatalf("stale cache shortened cooldown: %v %v", next, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyCadenceDoesNotReuseLegacySevenDayHold(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now().UTC()
	route, _ := ResolveCompanyRoute("grok")
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\)`).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(now.Add(-time.Hour)))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	next, err := (&EmailService{db: db, config: &config.Config{ThrottleDays: 7}}).companyThrottle(context.Background(), route, "support@x.ai", now)
	if err != nil || next != nil {
		t.Fatalf("old day throttle still suppresses new reports: %v %v", next, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyCadenceCanBeConfiguredWithoutChangingOtherThrottles(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now().UTC()
	last := now.Add(-time.Minute)
	route, _ := ResolveCompanyRoute("grok")
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\)`).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(last))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	next, err := (&EmailService{db: db, config: &config.Config{ThrottleDays: 17, CompanyNotificationInterval: 2 * time.Minute}}).companyThrottle(context.Background(), route, "support@x.ai", now)
	if err != nil || next == nil || !next.Equal(last.Add(2*time.Minute)) {
		t.Fatalf("configured company cadence ignored: %v %v", next, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func companyDailyArgs(route CompanyRoute, now time.Time) []driver.Value {
	start, end := companyDailyWindow(now)
	branch := companySQLArgs(route)
	branch = append(branch, start, end)
	args := append([]driver.Value(nil), branch...)
	args = append(args, branch...)
	return append(args, branch...)
}

func TestCompanyDailyCountAndRetryUseUTCMessageWindow(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	local := time.Date(2026, 10, 16, 0, 2, 0, 0, time.FixedZone("CEST", 2*60*60))
	route, _ := ResolveCompanyRoute("grok")
	start, end := companyDailyWindow(local)
	if !start.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("incorrect UTC window: %v %v", start, end)
	}
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs("support@x.ai").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\)`).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(nil))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*UNION ALL`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	mock.ExpectQuery(companyDailyQueryPattern).WithArgs(companyDailyArgs(route, local)...).WillReturnRows(sqlmock.NewRows([]string{"messages"}).AddRow(10))
	plan, err := (&EmailService{db: db, config: &config.Config{MaxDailyEmailsPerBrand: 10}}).buildCompanyPlan(context.Background(), route, local)
	if err != nil || plan.State != "daily_limit" || plan.RetryAt == nil || !plan.RetryAt.Equal(end) {
		t.Fatalf("daily message cap bypassed or retry misaligned: %#v %v", plan, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyFailureDoesNotStarveLaterCompanies(t *testing.T) {
	firstErr, secondErr := errors.New("receipt persistence failed"), errors.New("provider unavailable")
	var visited []string
	err := processCompanyRoutes(VerifiedCompanyRoutes(), func(route CompanyRoute) error {
		visited = append(visited, route.CompanyKey)
		if route.CompanyKey == "anthropic" {
			return firstErr
		}
		if route.CompanyKey == "openai" {
			return secondErr
		}
		return nil
	})
	if !reflect.DeepEqual(visited, []string{"anthropic", "openai", "xai"}) || !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("company failure starved another route: %v %v", visited, err)
	}
}

func TestCompanyWorkerDryRunNeverAcquiresLockOrMutates(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"reports": []any{}, "kind": "firsthand", "counts_exact": true, "scope": map[string]any{"brand_name": r.URL.Query().Get("brand_name")}})
	}))
	defer server.Close()
	for _, route := range VerifiedCompanyRoutes() {
		expectCompanyPolicyAllowed(mock, route)
	}
	s := &EmailService{db: db, config: &config.Config{DryRun: true, ReportListenerURL: server.URL}}
	if err := s.ProcessCompanyNotifications(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestKnownCompanyAliasesAreExcludedFromLegacyContactSelection(t *testing.T) {
	query := genericCompanyExclusionSQL("ra.brand_name")
	if !strings.Contains(query, "LOWER(TRIM(ra.brand_name))") {
		t.Fatal("legacy normalization differs from registry")
	}
	for _, route := range VerifiedCompanyRoutes() {
		for _, alias := range route.Aliases {
			if !strings.Contains(query, "'"+alias+"'") {
				t.Fatalf("legacy sender still owns known alias %q", alias)
			}
		}
	}
	for _, unknown := range []string{"twitter", "x.com", "x"} {
		if strings.Contains(query, "'"+unknown+"'") {
			t.Fatalf("unrelated brand excluded: %s", unknown)
		}
	}
}

func expectCompanyPending(mock sqlmock.Sqlmock, seqs, allowed []int64) {
	args := make([]driver.Value, len(seqs))
	for i, seq := range seqs {
		args[i] = seq
	}
	rows := sqlmock.NewRows([]string{"seq"})
	for _, seq := range allowed {
		rows.AddRow(seq)
	}
	mock.ExpectQuery(`(?s)SELECT r.seq FROM reports r.*ev.report_kind='firsthand'.*ev.entity_relationship='affected'.*ev.evidence_version>0.*sent.seq IS NULL.*ra.is_valid=TRUE.*visibility='public'.*report_status.*reports_owners.*retry.reason IN \('no_valid_emails','all_throttled'\).*notification_receipt_pending.*NOT EXISTS.*report_email_deliveries.*NOT EXISTS.*report_email_delivery_events`).WithArgs(args...).WillReturnRows(rows)
}

func TestCompanyPendingSelectionIsBoundedAndKeepsNewestTwenty(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	var candidates []PlannedCompanyReport
	var seqs []int64
	for i := int64(30); i > 0; i-- {
		candidates = append(candidates, PlannedCompanyReport{Seq: i})
		seqs = append(seqs, i)
	}
	expectCompanyPending(mock, seqs, seqs)
	got, err := (&EmailService{db: db}).pendingCompanyReports(context.Background(), candidates)
	if err != nil || len(got) != email.LinkedReportLimit || got[0].Seq != 30 || got[19].Seq != 11 {
		t.Fatalf("wrong selection: %#v %v", got, err)
	}
	if _, err := (&EmailService{db: db}).pendingCompanyReports(context.Background(), make([]PlannedCompanyReport, 51)); err == nil {
		t.Fatal("unbounded DB candidate query")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAllAutomaticDryRunsHaveNoDatabaseOrSendingEffects(t *testing.T) {
	// Nil dependencies make any accidental query/send fail immediately.
	s := &EmailService{config: &config.Config{DryRun: true}}
	if err := s.ProcessBrandNotifications(); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessBrandlessPhysicalReports(10); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessReports(); err != nil {
		t.Fatal(err)
	}
	if err := s.processReport(context.Background(), models.Report{Seq: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.processCompanyPlan(context.Background(), CompanyNotificationPlan{State: "ready", Reports: []PlannedCompanyReport{{Seq: 10}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCompanySendRechecksCanonicalSourceDateAndNeverSendsAgedReport(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now().UTC()
	route, _ := ResolveCompanyRoute("grok")
	server := companyFixtureServer(t, "xai", func() []companyReaderReport {
		return []companyReaderReport{companyFixture(10, now.Add(-31*24*time.Hour))}
	})
	defer server.Close()
	expectReceiptPreflight(mock)
	mock.ExpectQuery(`(?s)SELECT r.seq,r.public_id.*ORDER BY r.ts DESC,r.seq DESC LIMIT 20`).WillReturnRows(sqlmock.NewRows([]string{"seq", "public_id", "title", "summary", "excerpt", "impact", "kind", "classification", "ts"}).AddRow(10, "rpt_safe", "Issue", "Summary", "Original", "Impact", "firsthand", "digital", now))
	mock.ExpectQuery(`SELECT CASE WHEN OCTET_LENGTH`).WithArgs(email.MaxPreviewInputBytes, 10).WillReturnRows(sqlmock.NewRows([]string{"image"}).AddRow(nil))
	sender := &aggregateTestSender{}
	s := &EmailService{db: db, email: sender, config: &config.Config{ReportListenerURL: server.URL}}
	plan := CompanyNotificationPlan{Company: "xai", State: "ready", route: route, Reports: []PlannedCompanyReport{{Seq: 10, PublicID: "rpt_safe", EvidenceAt: now.Add(-time.Hour)}}}
	if err := s.processCompanyPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if len(sender.recipients) != 0 {
		t.Fatal("cached source date permitted stale evidence to send")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type companyCaptureSender struct {
	aggregateTestSender
	summary models.BrandReportSummary
}

func (s *companyCaptureSender) SendAggregateEmailsWithResults(to []string, summary *models.BrandReportSummary, optOut string) []email.CustomEmailSendResult {
	s.summary = *summary
	return s.aggregateTestSender.SendAggregateEmailsWithResults(to, summary, optOut)
}

func TestCompanySendUsesOnlyCurrentPendingFindingsAndVerifiedRecipient(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now().UTC()
	route, _ := ResolveCompanyRoute("grok")
	server := companyFixtureServer(t, "xai", func() []companyReaderReport {
		return []companyReaderReport{companyFixture(4, now.Add(-time.Minute)), companyFixture(3, now.Add(-time.Hour)), companyFixture(2, now.Add(-2*time.Hour)), companyFixture(1, now.Add(-3*time.Hour))}
	})
	defer server.Close()
	expectReceiptPreflight(mock)
	rows := sqlmock.NewRows([]string{"seq", "public_id", "title", "summary", "excerpt", "impact", "kind", "classification", "ts"})
	for i := 4; i > 0; i-- {
		rows.AddRow(i, "rpt_safe", "Issue", "Summary", "Original", "Impact", "firsthand", "digital", now)
	}
	mock.ExpectQuery(`(?s)SELECT r.seq,r.public_id.*ra.brand_name IN.*ORDER BY r.ts DESC,r.seq DESC LIMIT 20`).WillReturnRows(rows)
	for _, seq := range []int{4, 3, 2} {
		mock.ExpectQuery(`SELECT CASE WHEN OCTET_LENGTH`).WithArgs(email.MaxPreviewInputBytes, seq).WillReturnRows(sqlmock.NewRows([]string{"image"}).AddRow(nil))
	}
	// Report 3 was accepted/processed or held while the plan was being loaded.
	expectCompanyPending(mock, []int64{4, 3, 2, 1}, []int64{4, 2, 1})
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs("support@x.ai").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT MAX\(last_sent_at\).*WHERE LOWER\(TRIM\(email\)\)=\?`).WithArgs(companySQLArgs(route, "support@x.ai")...).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(nil))
	mock.ExpectQuery(`(?s)SELECT MAX\(sent_at\).*STRAIGHT_JOIN report_email_deliveries.*UNION ALL.*STRAIGHT_JOIN report_email_delivery_events`).WillReturnRows(sqlmock.NewRows([]string{"accepted"}).AddRow(nil))
	mock.ExpectQuery(companyDailyQueryPattern).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`INSERT INTO brand_email_throttle`).WithArgs("xai", "support@x.ai").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("support@x.ai").WillReturnResult(sqlmock.NewResult(1, 1))
	for _, seq := range []int64{4, 2, 1} {
		expectAcceptedEvent(mock, seq, "support@x.ai", "aggregate_report", now)
	}
	for _, seq := range []int64{4, 2, 1} {
		mock.ExpectExec(`INSERT INTO sent_reports_emails`).WithArgs(seq).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`DELETE FROM email_report_retry`).WithArgs(seq).WillReturnResult(sqlmock.NewResult(1, 1))
	}
	sender := &companyCaptureSender{aggregateTestSender: aggregateTestSender{at: now}}
	s := &EmailService{db: db, email: sender, config: &config.Config{ReportListenerURL: server.URL, MaxDailyEmailsPerBrand: 10}}
	plan := CompanyNotificationPlan{Company: "xai", State: "ready", route: route, total: 301}
	for _, seq := range []int64{4, 3, 2, 1} {
		plan.Reports = append(plan.Reports, PlannedCompanyReport{Seq: seq, PublicID: "rpt_safe", EvidenceAt: now.Add(-time.Hour)})
	}
	if err := s.processCompanyPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sender.recipients, []string{"support@x.ai"}) || !reflect.DeepEqual(sender.summary.ReportSeqs, []int64{4, 2, 1}) || len(sender.summary.Reports) != 3 || sender.summary.NewReportCount != 3 {
		t.Fatalf("wrong delivered scope: to=%v summary=%#v", sender.recipients, sender.summary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGenericDailyBudgetFailureCannotSend(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`GROUP BY ra.brand_name`).WillReturnRows(sqlmock.NewRows([]string{"brand", "display", "new", "total", "seqs", "latest", "classification", "emails"}).AddRow("acme", "Acme", 1, 1, "10:", 10, "digital", "a@company.com"))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM brand_email_throttle`).WithArgs("acme").WillReturnError(context.DeadlineExceeded)
	mock.ExpectExec(`INSERT INTO email_report_retry`).WithArgs(10, retryReasonThrottleError, sqlmock.AnyArg(), retryReasonNoValidEmails, 6, 5, 168, retryReasonNoValidEmails, 10).WillReturnResult(sqlmock.NewResult(1, 1))
	sender := &aggregateTestSender{}
	if err := (&EmailService{db: db, email: sender, config: &config.Config{MaxDailyEmailsPerBrand: 10}}).ProcessBrandNotifications(); err != nil {
		t.Fatal(err)
	}
	if len(sender.recipients) != 0 {
		t.Fatal("daily budget failure sent email")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessReportDoesNotMarkHeldInferredNotificationProcessed(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`(?s)SELECT seq, source, title, description.*FROM report_analysis`).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"seq", "source", "title", "description", "brand", "display", "litter", "hazard", "severity", "emails", "classification", "legal"}).AddRow(10, "fixture", "Issue", "Description", "", "", 0.0, 0.0, 0.5, "a@company.com", "digital", ""))
	expectIndividualPrior(mock, "a@company.com", 0)
	expectIndividualOptOut(mock, "a@company.com", 0)
	mock.ExpectQuery(`SELECT last_sent_at FROM brand_email_throttle`).WithArgs("__missing_brand__", "a@company.com").WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(time.Now()))
	expectIndividualRetry(mock, retryReasonAllThrottled)
	sender := &aggregateTestSender{}
	err := (&EmailService{db: db, email: sender, config: &config.Config{}}).processReport(context.Background(), models.Report{Seq: 10})
	if err == nil || len(sender.recipients) != 0 {
		t.Fatalf("held inferred report was completed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPriorReceiptCheckReadsBothLedgers(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery(`(?s)COUNT\(DISTINCT seq\).*report_email_deliveries.*UNION ALL.*report_email_delivery_events`).WithArgs("support@company.com", 1, 2, "support@company.com", 1, 2).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	got, err := (&EmailService{db: db}).alreadyNotifiedAllReports(context.Background(), []int64{1, 2}, "Support@Company.com")
	if err != nil || !got {
		t.Fatalf("append-only acceptance was ignored: %v %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyPlanPreviewContainsNoAddressesAndCannotMutate(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"reports": []any{}, "kind": "firsthand", "counts_exact": true, "scope": map[string]any{"brand_name": r.URL.Query().Get("brand_name")}})
	}))
	defer server.Close()
	for _, route := range VerifiedCompanyRoutes() {
		expectCompanyPolicyAllowed(mock, route)
	}
	plans := (&EmailService{db: db, config: &config.Config{ReportListenerURL: server.URL}}).BuildCompanyNotificationPlans(context.Background(), now)
	if len(plans) != 3 {
		t.Fatal("company plan scope changed")
	}
	for _, p := range plans {
		if p.State != "no_recent_reports" {
			t.Fatalf("unexpected plan: %#v", p)
		}
	}
	data, _ := json.Marshal(plans)
	if strings.Contains(string(data), "support@") || strings.Contains(string(data), "recipient_email") || !reflect.DeepEqual(plans[0].Reports, []PlannedCompanyReport{}) {
		t.Fatal("preview disclosed recipient data or invented reports")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
