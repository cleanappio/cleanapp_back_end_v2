package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// This opt-in fixture never uses app credentials and refuses remote/non-test DBs.
func TestReportNotificationsMySQLFixture(t *testing.T) {
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set NOTIFICATION_TEST_DSN to a dedicated local database ending _notification_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_notification_test") {
		t.Fatal("fixture only permits dedicated local databases ending _notification_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("fixture requires empty dedicated database: table count=%d error=%v", existing, err)
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	statements := []struct{ name, ddl string }{
		{"report_email_deliveries", `CREATE TABLE report_email_deliveries (id BIGINT AUTO_INCREMENT PRIMARY KEY,seq INT NOT NULL,recipient_email VARCHAR(255) NOT NULL,delivery_status VARCHAR(32) NOT NULL,delivery_source VARCHAR(64) NOT NULL,sent_at TIMESTAMP NULL,UNIQUE KEY(seq,recipient_email),KEY(seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"reports", `CREATE TABLE reports (seq INT PRIMARY KEY,public_id VARCHAR(40),image BLOB,UNIQUE KEY(public_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"report_raw", `CREATE TABLE report_raw (report_seq INT PRIMARY KEY,visibility VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"report_status", `CREATE TABLE report_status (seq INT PRIMARY KEY,status VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"reports_owners", `CREATE TABLE reports_owners (seq INT,owner VARCHAR(64),is_public BOOL,KEY(seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"subscriptions", `CREATE TABLE subscriptions (id INT PRIMARY KEY,customer_id VARCHAR(64),status VARCHAR(32),plan_type VARCHAR(32),start_date DATE,KEY(customer_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.ddl); err != nil {
			t.Fatal(err)
		}
		created = append(created, statement.name)
	}
	if err := ensureReportEmailDeliveryEvents(ctx, db); err != nil {
		t.Fatal(err)
	}
	created = append(created, "report_email_delivery_events")
	if err := ensureReportEmailDeliveryEvents(ctx, db); err != nil {
		t.Fatalf("migration must be idempotent: %v", err)
	}
	seed := []string{
		`INSERT INTO report_email_deliveries VALUES (1,1,'support@x.ai','sent','aggregate_report','2026-10-15 18:00:00'),(2,1,'contact@x.ai','sent','aggregate_brand',NULL),(3,2,'private@x.ai','failed','aggregate_report','2026-10-15 18:00:00')`,
		`INSERT INTO report_email_delivery_events (seq,recipient_email,delivery_status,delivery_source,sent_at,legacy_delivery_id,is_legacy) VALUES (1,'support@x.ai','sent','aggregate_brand','2026-09-21 18:36:41',1,TRUE),(1,'support@x.ai','sent','aggregate_report','2026-10-15 18:00:00',NULL,FALSE),(2,'support@x.ai','sent','aggregate_report','2026-10-15 18:00:00',NULL,FALSE)`,
		`INSERT INTO reports VALUES (1,'rpt_public','public-image'),(2,'rpt_private','private-image'),(3,'rpt_shadow','shadow-image'),(4,'rpt_resolved','resolved-image')`,
		`INSERT INTO reports_owners VALUES (2,'owner',FALSE)`,
		`INSERT INTO report_raw VALUES (3,'shadow')`,
		`INSERT INTO report_status VALUES (4,'resolved')`,
		`INSERT INTO subscriptions VALUES (1,'active-base','active','base','2026-01-01'),(2,'expired','cancelled','exclusive','2026-01-01'),(3,'free','active','free','2026-01-01'),(4,'future','active','exclusive','2099-01-01')`,
	}
	for _, query := range seed {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	database := &Database{db: db}
	history, err := database.ReportNotificationHistory(ctx, 1)
	if err != nil || history.EmailCount != 3 || history.RecipientCount != 2 || history.HistoryComplete {
		t.Fatalf("legacy/new receipt union counted incorrectly: %+v error=%v", history, err)
	}
	// Reusing a legacy row's ID must not hide a later send recorded by an older writer.
	if _, err := db.Exec(`UPDATE report_email_deliveries SET delivery_source='area_contact',sent_at='2026-10-16 18:00:00' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	history, err = database.ReportNotificationHistory(ctx, 1)
	if err != nil || history.EmailCount != 4 || history.RecipientCount != 2 {
		t.Fatalf("unmatched legacy update was lost: %+v error=%v", history, err)
	}
	history, err = database.ReportNotificationHistory(ctx, 2)
	if err != nil || history.EmailCount != 1 || !history.HistoryComplete || len(history.PreviewRecipients) != 1 {
		t.Fatalf("failed legacy row must not count: %+v error=%v", history, err)
	}
	for _, publicID := range []string{"rpt_private", "rpt_shadow", "rpt_resolved", "rpt_missing"} {
		if _, err := database.PublicReportSequence(ctx, publicID); err != sql.ErrNoRows {
			t.Fatalf("nonpublic report %s disclosed notification existence: %v", publicID, err)
		}
		if image, err := database.GetImageByPublicID(ctx, publicID); err == nil || len(image) != 0 {
			t.Fatalf("nonpublic report %s disclosed image: %q, %v", publicID, image, err)
		}
	}
	if seq, err := database.PublicReportSequence(ctx, "rpt_public"); err != nil || seq != 1 {
		t.Fatalf("public report resolution: %d %v", seq, err)
	}
	if image, err := database.GetImageByPublicID(ctx, "rpt_public"); err != nil || string(image) != "public-image" {
		t.Fatalf("public image: %q %v", image, err)
	}
	if image, err := database.GetPublicReportMediaByPublicID(ctx, "rpt_public", 100); err != nil || string(image) != "public-image" {
		t.Fatalf("bounded public media: %q %v", image, err)
	}
	if image, err := database.GetPublicReportMediaByPublicID(ctx, "rpt_public", 4); err != nil || len(image) != 0 {
		t.Fatalf("oversized media must not transfer its blob: %q %v", image, err)
	}
	// A token can outlive publication. Every later image read must apply current policy.
	if _, err := db.Exec(`INSERT INTO reports_owners VALUES (1,'new-private-owner',FALSE)`); err != nil {
		t.Fatal(err)
	}
	if image, err := database.GetImageByPublicID(ctx, "rpt_public"); err == nil || len(image) != 0 {
		t.Fatalf("previously public image remained available after privatization: %q %v", image, err)
	}
	if image, err := database.GetPublicReportMediaByPublicID(ctx, "rpt_public", 100); err == nil || len(image) != 0 {
		t.Fatalf("previously public preview remained available after privatization: %q %v", image, err)
	}
	for _, userID := range []string{"active-base", "expired", "free", "future", "missing"} {
		got, err := database.HasActiveNotificationSubscription(ctx, userID)
		if err != nil || got != (userID == "active-base") {
			t.Fatalf("subscription %s: %v %v", userID, got, err)
		}
	}
}
