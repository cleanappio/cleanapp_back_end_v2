package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"email-service/config"
	"email-service/email"
	"email-service/models"
	"github.com/go-sql-driver/mysql"
)

// This fixture only runs against an explicitly provisioned, empty local test DB.
// It never creates a connection to production or mutates an existing table.
func notificationMySQLFixture(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("EMAIL_NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("EMAIL_NOTIFICATION_TEST_DSN is not configured")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.ToLower(cfg.Addr)
	if cfg.Net != "tcp" || !(strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_notification_test") {
		t.Fatal("fixture requires localhost TCP and a *_notification_test database")
	}
	cfg.ParseTime = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	var sessionZone string
	if err := db.QueryRow(`SELECT @@session.time_zone`).Scan(&sessionZone); err != nil || sessionZone != "+00:00" {
		db.Close()
		t.Fatalf("fixture must establish UTC on every session: %q %v", sessionZone, err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`CREATE TABLE reports(seq INT PRIMARY KEY,public_id VARCHAR(32),ts DATETIME,image BLOB)`,
		`CREATE TABLE report_analysis(seq INT PRIMARY KEY,language VARCHAR(8),brand_name VARCHAR(100),brand_display_name VARCHAR(100),title TEXT,summary TEXT,classification VARCHAR(32),inferred_contact_emails TEXT,is_valid BOOLEAN)`,
		`CREATE TABLE report_raw(report_seq INT PRIMARY KEY,visibility VARCHAR(32))`,
		`CREATE TABLE report_status(seq INT PRIMARY KEY,status VARCHAR(32))`,
		`CREATE TABLE reports_owners(seq INT,owner VARCHAR(100),is_public BOOLEAN,PRIMARY KEY(seq,owner))`,
		`CREATE TABLE report_evidence(seq INT PRIMARY KEY,report_kind VARCHAR(32),entity_relationship VARCHAR(32) DEFAULT 'affected',evidence_version INT DEFAULT 1,evidence_excerpt TEXT,reported_impact TEXT)`,
		`CREATE TABLE sent_reports_emails(seq INT PRIMARY KEY)`,
		`CREATE TABLE email_report_retry(seq INT PRIMARY KEY,next_attempt_at DATETIME,reason VARCHAR(64))`,
		`CREATE TABLE brand_email_throttle(brand_name VARCHAR(255),email VARCHAR(255),last_sent_at DATETIME,PRIMARY KEY(brand_name,email))`,
		`CREATE TABLE report_email_deliveries(id BIGINT AUTO_INCREMENT PRIMARY KEY,seq INT,recipient_email VARCHAR(255),delivery_status VARCHAR(32),delivery_source VARCHAR(64),provider VARCHAR(32),sent_at TIMESTAMP NULL,updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,UNIQUE KEY uq_report_email_delivery(seq,recipient_email)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_email_delivery_events(id BIGINT AUTO_INCREMENT PRIMARY KEY,seq INT,recipient_email VARCHAR(320),delivery_status VARCHAR(32),delivery_source VARCHAR(64),provider VARCHAR(64),provider_message_id VARCHAR(255),sent_at TIMESTAMP NULL,legacy_delivery_id BIGINT NULL,is_legacy BOOLEAN DEFAULT FALSE,receipt_key CHAR(64) NULL,UNIQUE KEY(receipt_key),UNIQUE KEY(legacy_delivery_id,sent_at,delivery_source),KEY idx_report_delivery_event_seq(seq,sent_at,id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatal("fixture requires an empty database")
	}
	var created []string
	t.Cleanup(func() {
		for i := len(created) - 1; i >= 0; i-- {
			if _, err := db.Exec("DROP TABLE " + created[i]); err != nil {
				t.Error(err)
			}
		}
	})
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
		created = append(created, strings.Split(q, "(")[0][len("CREATE TABLE "):])
	}
	return db
}

func TestNotificationMySQL(t *testing.T) {
	db := notificationMySQLFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	statements := []string{
		`INSERT INTO reports(seq,public_id,ts,image) VALUES(1,'rpt_old','2025-01-01',NULL),(2,'rpt_recent','2026-10-15',NULL),(3,'rpt_private','2026-10-16',NULL),(4,'rpt_inactive','2026-10-17',NULL),(5,'rpt_shadow','2026-10-18',NULL),(6,'rpt_invalid','2026-10-19',NULL),(7,'rpt_processed','2026-10-20',NULL)`,
		`INSERT INTO report_analysis SELECT seq,'en','acme','Acme',CONCAT('Issue ',seq),'Summary','digital','support@company.com',seq<>6 FROM reports`,
		`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact) SELECT seq,IF(seq=1,'firsthand','coverage'),CONCAT('Original ',seq),'Impact' FROM reports`,
		`INSERT INTO reports_owners VALUES(3,'private-owner',FALSE)`,
		`INSERT INTO report_status VALUES(4,'resolved')`,
		`INSERT INTO report_raw VALUES(5,'shadow')`,
		`INSERT INTO sent_reports_emails VALUES(7)`,
		`INSERT INTO report_email_deliveries(seq,recipient_email,delivery_status,delivery_source,provider,sent_at) VALUES(2,'support@company.com','sent','aggregate_brand','sendgrid','2026-09-21 18:36:41')`,
	}
	for _, q := range statements {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s := &EmailService{db: db}
	if err := s.preflightNotificationReceipts(ctx); err != nil {
		t.Fatal(err)
	}
	summaries, err := s.getUnprocessedReportsByBrand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].NewReportCount != 2 || summaries[0].TotalReportCount != 3 || len(summaries[0].ReportSeqs) != 2 {
		t.Fatalf("private/inactive/invalid/processed reports leaked into batch: %#v", summaries)
	}
	findings, err := s.getAggregateFindings(ctx, &summaries[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].Seq != 2 || findings[1].Seq != 1 || findings[0].OriginalExcerpt != "Original 2" {
		t.Fatalf("newest public report did not lead: %#v", findings)
	}
	// The explicit candidates themselves are also checked, rather than trusting
	// that the earlier brand query removed private reports.
	findings, err = s.getAggregateFindings(ctx, &models.BrandReportSummary{BrandName: "acme", ReportSeqs: []int64{3, 4, 5, 6}})
	if err != nil || len(findings) != 0 {
		t.Fatalf("private/inactive/shadow/invalid evidence leaked: %#v %v", findings, err)
	}
	for i := 0; i < 2; i++ {
		at := time.Date(2026, 10, 15, 12, i, 0, 0, time.UTC)
		if err := s.recordReportEmailDeliveryResult(ctx, 2, "support@company.com", "aggregate_report", email.CustomEmailSendResult{Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted", SentAt: at}); err != nil {
			t.Fatal(err)
		}
		if i == 0 { // Simulate a legacy-only writer updating the same old row ID.
			if _, err := db.ExecContext(ctx, `UPDATE report_email_deliveries SET delivery_source='aggregate_brand',provider='sendgrid',sent_at='2026-10-15 12:00:30' WHERE seq=2`); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Replaying the same receipt after a lost commit acknowledgement is idempotent.
	if err := s.recordReportEmailDeliveryResult(ctx, 2, "support@company.com", "aggregate_report", email.CustomEmailSendResult{Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted", SentAt: time.Date(2026, 10, 15, 12, 1, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	var count, legacy, accepted int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),SUM(is_legacy),SUM(provider_message_id='accepted') FROM report_email_delivery_events WHERE seq=2`).Scan(&count, &legacy, &accepted); err != nil {
		t.Fatal(err)
	}
	if count != 4 || legacy != 2 || accepted != 2 {
		t.Fatalf("prior notification or resend lost: count=%d legacy=%d accepted=%d", count, legacy, accepted)
	}
	var original int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_email_delivery_events WHERE seq=2 AND delivery_source='aggregate_brand' AND sent_at='2026-09-21 18:36:41'`).Scan(&original); err != nil || original != 1 {
		t.Fatalf("original digest was rewritten/lost: count=%d %v", original, err)
	}
	// The short company cadence can supersede obsolete no-contact and seven-day
	// throttle delays for a report that has never been accepted or processed.
	// Successful prior delivery, processed rows, privacy, and every other hold
	// remain authoritative even when a verified recipient has become available.
	for _, q := range []string{
		`INSERT INTO reports(seq,public_id,ts) VALUES(8,'rpt_8',NOW()),(9,'rpt_9',NOW()),(10,'rpt_10',NOW()),(11,'rpt_11',NOW()),(12,'rpt_12',NOW()),(13,'rpt_13',NOW()),(14,'rpt_14',NOW()),(15,'rpt_15',NOW()),(16,'rpt_16',NOW()),(17,'rpt_17',NOW()),(18,'rpt_18',NOW()),(19,'rpt_19',NOW()),(20,'rpt_unknown_history',NOW())`,
		`INSERT INTO report_analysis SELECT seq,'en','grok','Grok','Issue','Summary','digital','',TRUE FROM reports WHERE seq>=8`,
		`INSERT INTO report_evidence(seq,report_kind,entity_relationship,evidence_version) SELECT seq,IF(seq=17,'coverage','firsthand'),IF(seq=18,'dependency','affected'),IF(seq=19,0,1) FROM reports WHERE seq>=8`,
		`INSERT INTO email_report_retry VALUES(9,DATE_ADD(NOW(),INTERVAL 30 DAY),'no_valid_emails'),(10,DATE_ADD(NOW(),INTERVAL 30 DAY),'all_throttled'),(11,DATE_ADD(NOW(),INTERVAL 30 DAY),'daily_limit'),(12,'2020-01-01','notification_receipt_pending')`,
		`INSERT INTO report_email_deliveries(seq,recipient_email,delivery_status,delivery_source,provider,sent_at) VALUES(13,'prior@company.com','sent','aggregate_brand','sendgrid',NOW()),(20,'prior@company.com','sent','aggregate_brand','sendgrid',NULL)`,
		`INSERT INTO report_email_delivery_events(seq,recipient_email,delivery_status,delivery_source,provider,sent_at) VALUES(14,'prior@company.com','sent','aggregate_report','sendgrid',NOW())`,
		`INSERT INTO sent_reports_emails VALUES(15)`,
		`INSERT INTO reports_owners VALUES(16,'owner',FALSE)`,
		`INSERT INTO brand_email_throttle VALUES(' Claude Code ',' Support@Anthropic.com ','2026-10-09 22:57:17'),('claude','other@anthropic.com','2026-10-15 23:59:59')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var candidates []PlannedCompanyReport
	for seq := int64(8); seq <= 20; seq++ {
		candidates = append(candidates, PlannedCompanyReport{Seq: seq})
	}
	pending, err := s.pendingCompanyReports(ctx, candidates)
	if err != nil || len(pending) != 3 || pending[0].Seq != 8 || pending[1].Seq != 9 || pending[2].Seq != 10 {
		t.Fatalf("company recovery bypassed protected state: %#v %v", pending, err)
	}
	route, _ := ResolveCompanyRoute("anthropic")
	s.config = &config.Config{ThrottleDays: 7, CompanyNotificationInterval: 5 * time.Minute}
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	// The generic seven-day setting no longer postpones new company reports.
	if next, err := s.companyThrottle(ctx, route, "support@anthropic.com", now); err != nil || next != nil {
		t.Fatalf("obsolete seven-day company cooldown survived: %v %v", next, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE brand_email_throttle SET last_sent_at=? WHERE brand_name=' Claude Code '`, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	next, err := s.companyThrottle(ctx, route, "support@anthropic.com", now)
	if err != nil || next == nil || !next.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("official recipient alias cadence changed: %v %v", next, err)
	}
	if next, err := s.companyThrottle(ctx, route, "support@anthropic.com", now.Add(3*time.Minute)); err != nil || next != nil {
		t.Fatalf("expired short cadence still held a company: %v %v", next, err)
	}
	// A recent accepted event is sufficient when the mutable throttle cache
	// failed completely, and extends an older cached cadence across aliases.
	if _, err := db.ExecContext(ctx, `INSERT INTO report_email_delivery_events(seq,recipient_email,delivery_status,delivery_source,provider,sent_at) VALUES(8,'support@x.ai','sent','aggregate_report','google_workspace',?)`, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, cache := range []bool{false, true} {
		if cache {
			if _, err := db.ExecContext(ctx, `INSERT INTO brand_email_throttle VALUES('grok','support@x.ai',?)`, now.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		xai, _ := ResolveCompanyRoute("grok")
		next, err := s.companyThrottle(ctx, xai, "support@x.ai", now)
		if err != nil || next == nil || !next.Equal(now.Add(4*time.Minute)) {
			t.Fatalf("accepted ledger did not retain short cadence (cache=%v): %v %v", cache, next, err)
		}
		if next, err := s.companyThrottle(ctx, xai, "support@x.ai", now.Add(4*time.Minute)); err != nil || next != nil {
			t.Fatalf("expired durable receipt still imposed a seven-day hold: %v %v", next, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO email_report_retry(seq,next_attempt_at,reason) VALUES(1,'2020-01-01','notification_receipt_pending'),(2,'2020-01-01','notification_receipt_pending')`); err != nil {
		t.Fatal(err)
	}
	quarantined, err := s.getUnprocessedReportsByBrand(ctx)
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("receipt recovery batch re-entered automatic send queue: %#v %v", quarantined, err)
	}
	t.Run("ActualCompanyMessagesPerUTCDay", func(t *testing.T) {
		route, _ := ResolveCompanyRoute("openai")
		day := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
		start, end := companyDailyWindow(day)
		at := start.Add(12 * time.Hour)
		for seq := int64(100); seq < 112; seq++ {
			if _, err := db.ExecContext(ctx, `INSERT INTO reports(seq,public_id,ts) VALUES(?,'rpt_budget',?)`, seq, at); err != nil {
				t.Fatal(err)
			}
			brand := "openai"
			if seq%2 == 1 {
				brand = "chat gpt"
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO report_analysis VALUES(?,'en',?,'OpenAI','Issue','Summary','digital','',TRUE)`, seq, brand); err != nil {
				t.Fatal(err)
			}
		}
		check := func(want int) {
			t.Helper()
			got, err := s.companyDailyCount(ctx, route, day)
			if err != nil || got != want {
				t.Fatalf("actual UTC-day message count=%d want=%d error=%v", got, want, err)
			}
		}
		accept := func(seq int64, recipient, provider, id string, when time.Time) {
			t.Helper()
			if err := s.recordReportEmailDeliveryResult(ctx, seq, recipient, "aggregate_report", email.CustomEmailSendResult{Status: "sent", Provider: provider, ProviderMessageID: id, SentAt: when}); err != nil {
				t.Fatal(err)
			}
		}
		check(0)
		// One SMTP acceptance directly links four reports. Each report has an
		// append event and latest legacy copy, but the daily budget uses one.
		for seq := int64(100); seq < 104; seq++ {
			accept(seq, "support@openai.com", "google_workspace", "message-A", at)
		}
		check(1)
		var eventCopies, latestCopies int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_email_delivery_events WHERE seq BETWEEN 100 AND 103`).Scan(&eventCopies); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_email_deliveries WHERE seq BETWEEN 100 AND 103`).Scan(&latestCopies); err != nil {
			t.Fatal(err)
		}
		if eventCopies != 4 || latestCopies != 4 {
			t.Fatal("fixture did not create per-report event and legacy copies")
		}
		// Distinct opaque IDs are separate actual sends, even at the same
		// timestamp. Their case and provider identity must be preserved.
		accept(100, "support@openai.com", "google_workspace", "message-B", at)
		check(2)
		accept(100, "support@openai.com", "google_workspace", "message-a", at)
		check(3)
		accept(101, "support@openai.com", "sendgrid", "message-A", at)
		check(4)
		// ID-less historical batch snapshots share recipient/source/time, and
		// the same recipient at a different time is a new recorded message.
		for seq := int64(104); seq < 106; seq++ {
			accept(seq, "support@openai.com", "google_workspace", "", at.Add(10*time.Second))
		}
		check(5)
		accept(104, "support@openai.com", "google_workspace", "", at.Add(11*time.Second))
		check(6)
		accept(105, "other@openai.com", "google_workspace", "message-A", at)
		check(7)
		accept(106, "support@openai.com", "google_workspace", "midnight", start)
		check(8)
		for _, row := range []struct {
			seq                   int64
			recipient, status, id string
			when                  any
		}{
			{107, "support@openai.com", "failed", "failed", at},
			{108, " \t\n", "sent", "blank-recipient", at},
			{109, "support@openai.com", "sent", "unknown-time", nil},
			{110, "support@openai.com", "sent", "previous-day", start.Add(-time.Second)},
			{111, "support@openai.com", "sent", "next-day", end},
			{1, "support@openai.com", "sent", "wrong-company", at},
		} {
			if _, err := db.ExecContext(ctx, `INSERT INTO report_email_delivery_events(seq,recipient_email,delivery_status,delivery_source,provider,provider_message_id,sent_at) VALUES(?,?,?,'aggregate_report','google_workspace',?,?)`, row.seq, row.recipient, row.status, row.id, row.when); err != nil {
				t.Fatal(err)
			}
		}
		check(8)
		// Known cache snapshots add nothing. Unledgered cache snapshots count
		// conservatively, with duplicate company aliases collapsed.
		for _, brand := range []string{"openai", "chatgpt"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO brand_email_throttle VALUES(?,'support@openai.com',?)`, brand, at); err != nil {
				t.Fatal(err)
			}
		}
		check(8)
		// Cache recording follows transport acceptance by a few seconds. The
		// documented five-second tolerance must not spend a second budget slot;
		// a six-second unmatched cache snapshot remains conservative.
		for _, offset := range []time.Duration{time.Second, 5 * time.Second} {
			if _, err := db.ExecContext(ctx, `UPDATE brand_email_throttle SET last_sent_at=? WHERE brand_name='openai'`, start.Add(offset)); err != nil {
				t.Fatal(err)
			}
			check(8)
		}
		if _, err := db.ExecContext(ctx, `UPDATE brand_email_throttle SET last_sent_at=? WHERE brand_name='openai'`, start.Add(6*time.Second)); err != nil {
			t.Fatal(err)
		}
		check(9)
		if _, err := db.ExecContext(ctx, `UPDATE brand_email_throttle SET last_sent_at=? WHERE brand_name='openai'`, at); err != nil {
			t.Fatal(err)
		}
		check(8)
		unledgered := at.Add(15 * time.Minute)
		if _, err := db.ExecContext(ctx, `UPDATE brand_email_throttle SET last_sent_at=? WHERE brand_name='openai'`, unledgered); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO brand_email_throttle VALUES('codex','support@openai.com',?)`, unledgered); err != nil {
			t.Fatal(err)
		}
		check(9)
		if _, err := db.ExecContext(ctx, `INSERT INTO brand_email_throttle VALUES('whisper','legacy@openai.com',?)`, unledgered); err != nil {
			t.Fatal(err)
		}
		check(10)
		if got, err := s.companyDailyCount(ctx, route, day.In(time.FixedZone("west", -7*60*60))); err != nil || got != 10 {
			t.Fatalf("caller timezone changed the UTC budget: %d %v", got, err)
		}
		// Legacy-only batch records remain countable without provider IDs.
		// Capturing an exact old row in the append ledger must not count it twice.
		legacyAt := at.Add(30 * time.Minute)
		if _, err := db.ExecContext(ctx, `INSERT INTO report_email_deliveries(seq,recipient_email,delivery_status,delivery_source,provider,sent_at) VALUES(106,'legacy-only@openai.com','sent','aggregate_brand','sendgrid',?),(107,'legacy-only@openai.com','sent','aggregate_brand','sendgrid',?)`, legacyAt, legacyAt); err != nil {
			t.Fatal(err)
		}
		check(11)
		if _, err := db.ExecContext(ctx, `INSERT INTO report_email_delivery_events(seq,recipient_email,delivery_status,delivery_source,provider,sent_at,is_legacy) VALUES(106,'legacy-only@openai.com','sent','aggregate_brand','sendgrid',?,TRUE)`, legacyAt); err != nil {
			t.Fatal(err)
		}
		check(11)
		if got, err := s.companyDailyCount(ctx, route, end); err != nil || got != 1 {
			t.Fatalf("UTC day boundary counted old or lost midnight messages: %d %v", got, err)
		}
	})

	t.Run("CanonicalCompanyLock", func(t *testing.T) {
		lockCtx, lockCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer lockCancel()
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		failure := errors.New("controlled callback failure")
		type lockResult struct {
			ran bool
			err error
		}
		done := make(chan lockResult, 1)
		go func() {
			ran, err := s.withCompanyNotificationLock(lockCtx, "x.ai", func() error {
				// The lock owns a separate connection; ordinary callback queries
				// must still have a usable pool connection.
				var one int
				if err := db.QueryRowContext(lockCtx, `SELECT 1`).Scan(&one); err != nil {
					return err
				}
				close(entered)
				select {
				case <-release:
					return failure
				case <-lockCtx.Done():
					return lockCtx.Err()
				}
			})
			done <- lockResult{ran, err}
		}()
		select {
		case <-entered:
		case <-lockCtx.Done():
			t.Fatal("first worker did not acquire its company lock")
		}
		busyCalled := false
		ran, err := s.withCompanyNotificationLock(lockCtx, "  Grok  ", func() error { busyCalled = true; return nil })
		if err != nil || ran || busyCalled {
			t.Fatalf("canonical aliases ran concurrently: ran=%v callback=%v error=%v", ran, busyCalled, err)
		}
		otherCalled := false
		ran, err = s.withCompanyNotificationLock(lockCtx, "anthropic", func() error { otherCalled = true; return nil })
		if err != nil || !ran || !otherCalled {
			t.Fatalf("unrelated company was locked: %v %v", ran, err)
		}
		unblock()
		select {
		case result := <-done:
			if !result.ran || !errors.Is(result.err, failure) {
				t.Fatalf("callback outcome lost: %+v", result)
			}
		case <-lockCtx.Done():
			t.Fatal("failing callback did not release its lock")
		}
		afterCalled := false
		ran, err = s.withCompanyNotificationLock(lockCtx, "xai", func() error { afterCalled = true; return nil })
		if err != nil || !ran || !afterCalled {
			t.Fatalf("callback failure stranded the company lock: %v %v", ran, err)
		}
	})

}
