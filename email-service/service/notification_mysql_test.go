package service

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

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
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`CREATE TABLE reports(seq INT PRIMARY KEY,public_id VARCHAR(32),ts DATETIME,image BLOB)`,
		`CREATE TABLE report_analysis(seq INT PRIMARY KEY,language VARCHAR(8),brand_name VARCHAR(100),brand_display_name VARCHAR(100),title TEXT,summary TEXT,classification VARCHAR(32),inferred_contact_emails TEXT,is_valid BOOLEAN)`,
		`CREATE TABLE report_raw(report_seq INT PRIMARY KEY,visibility VARCHAR(32))`,
		`CREATE TABLE report_status(seq INT PRIMARY KEY,status VARCHAR(32))`,
		`CREATE TABLE reports_owners(seq INT,owner VARCHAR(100),is_public BOOLEAN,PRIMARY KEY(seq,owner))`,
		`CREATE TABLE report_evidence(seq INT PRIMARY KEY,report_kind VARCHAR(32),evidence_excerpt TEXT,reported_impact TEXT)`,
		`CREATE TABLE sent_reports_emails(seq INT PRIMARY KEY)`,
		`CREATE TABLE email_report_retry(seq INT PRIMARY KEY,next_attempt_at DATETIME,reason VARCHAR(64))`,
		`CREATE TABLE report_email_deliveries(id BIGINT AUTO_INCREMENT PRIMARY KEY,seq INT,recipient_email VARCHAR(255),delivery_status VARCHAR(32),delivery_source VARCHAR(64),provider VARCHAR(32),sent_at TIMESTAMP NULL,updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,UNIQUE KEY(seq,recipient_email))`,
		`CREATE TABLE report_email_delivery_events(id BIGINT AUTO_INCREMENT PRIMARY KEY,seq INT,recipient_email VARCHAR(320),delivery_status VARCHAR(32),delivery_source VARCHAR(64),provider VARCHAR(64),provider_message_id VARCHAR(255),sent_at TIMESTAMP NULL,legacy_delivery_id BIGINT NULL,is_legacy BOOLEAN DEFAULT FALSE,receipt_key CHAR(64) NULL,UNIQUE KEY(receipt_key),UNIQUE KEY(legacy_delivery_id,sent_at,delivery_source),KEY(seq,sent_at,id))`,
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

func TestNotificationMySQLPrivacyNewestSelectionAndAppendOnlyResends(t *testing.T) {
	db := notificationMySQLFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	statements := []string{
		`INSERT INTO reports(seq,public_id,ts,image) VALUES(1,'rpt_old','2025-01-01',NULL),(2,'rpt_recent','2026-10-15',NULL),(3,'rpt_private','2026-10-16',NULL),(4,'rpt_inactive','2026-10-17',NULL),(5,'rpt_shadow','2026-10-18',NULL),(6,'rpt_invalid','2026-10-19',NULL),(7,'rpt_processed','2026-10-20',NULL)`,
		`INSERT INTO report_analysis SELECT seq,'en','acme','Acme',CONCAT('Issue ',seq),'Summary','digital','support@company.com',seq<>6 FROM reports`,
		`INSERT INTO report_evidence SELECT seq,IF(seq=1,'firsthand','coverage'),CONCAT('Original ',seq),'Impact' FROM reports`,
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
	if _, err := db.ExecContext(ctx, `INSERT INTO email_report_retry(seq,next_attempt_at,reason) VALUES(1,'2020-01-01','notification_receipt_pending'),(2,'2020-01-01','notification_receipt_pending')`); err != nil {
		t.Fatal(err)
	}
	quarantined, err := s.getUnprocessedReportsByBrand(ctx)
	if err != nil || len(quarantined) != 0 {
		t.Fatalf("receipt recovery batch re-entered automatic send queue: %#v %v", quarantined, err)
	}
}
