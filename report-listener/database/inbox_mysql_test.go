package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"report-listener/inbox"

	"github.com/go-sql-driver/mysql"
)

// This opt-in fixture never uses app credentials and refuses remote/non-test DBs.
func TestInboxMessagesMySQLFixture(t *testing.T) {
	dsn := os.Getenv("INBOX_TEST_DSN")
	if dsn == "" {
		t.Skip("set INBOX_TEST_DSN to a dedicated local database ending _inbox_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_inbox_test") {
		t.Fatal("fixture only permits dedicated local databases ending _inbox_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("fixture requires empty dedicated database: table count=%d error=%v", existing, err)
	}
	created := []string{"reports", "notification_messages", "notification_message_reports", "opted_out_emails", "notify_outcomes", "contact_endpoint_memory", "inbox_messages"}
	defer func() {
		for _, table := range created {
			_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		}
	}()
	// Tables owned by other migrations, in the shape production has them.
	for _, ddl := range []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY, public_id VARCHAR(40)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE notification_messages (notification_id CHAR(16) NOT NULL PRIMARY KEY, provider_message_id VARCHAR(255) NOT NULL DEFAULT '', recipient_email VARCHAR(320) NOT NULL, company_key VARCHAR(64) NULL, thread_key VARCHAR(64) NULL, sent_at TIMESTAMP NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, INDEX (provider_message_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE notification_message_reports (notification_id CHAR(16) NOT NULL, seq INT NOT NULL, PRIMARY KEY (notification_id, seq)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE opted_out_emails (id INT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE, opted_out_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE notify_outcomes (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, subject_kind VARCHAR(16) NOT NULL, subject_ref VARCHAR(128) NOT NULL, target_id BIGINT UNSIGNED NULL, endpoint_key VARCHAR(255) NOT NULL DEFAULT '', outcome_type VARCHAR(32) NOT NULL, source_type VARCHAR(32) NOT NULL DEFAULT '', source_ref VARCHAR(255) NOT NULL DEFAULT '', evidence_json JSON NULL, recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (id), KEY (subject_kind, subject_ref, recorded_at)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE contact_endpoint_memory (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, endpoint_key VARCHAR(255) NOT NULL, organization_key VARCHAR(255) NOT NULL DEFAULT '', channel_type VARCHAR(32) NOT NULL DEFAULT 'email', channel_value VARCHAR(512) NOT NULL, last_result VARCHAR(32) NOT NULL DEFAULT '', success_count INT NOT NULL DEFAULT 0, bounce_count INT NOT NULL DEFAULT 0, ack_count INT NOT NULL DEFAULT 0, fix_count INT NOT NULL DEFAULT 0, misroute_count INT NOT NULL DEFAULT 0, no_response_count INT NOT NULL DEFAULT 0, last_contacted_at TIMESTAMP NULL, cooldown_until TIMESTAMP NULL, preferred_for_role_type VARCHAR(64) NOT NULL DEFAULT '', preferred_for_asset_class VARCHAR(64) NOT NULL DEFAULT '', updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, PRIMARY KEY (id), UNIQUE KEY (endpoint_key)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := ensureInboxMessagesTable(ctx, db); err != nil {
			t.Fatalf("migration must be idempotent: %v", err)
		}
	}
	for _, query := range []string{
		`INSERT INTO reports VALUES (1,'rpt_one'),(2,'rpt_two'),(3,'')`,
		`INSERT INTO notification_messages (notification_id, provider_message_id, recipient_email, company_key, sent_at) VALUES ('0123456789abcdef','<relay-1@mail.gmail.com>','Support@Example.com','example','2026-11-10 10:00:00'),('fedcba9876543210','relay-2@mail.gmail.com','help@other.example','other','2026-11-11 10:00:00')`,
		`INSERT INTO notification_message_reports VALUES ('0123456789abcdef',1),('0123456789abcdef',2),('0123456789abcdef',3),('fedcba9876543210',2)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	d := &Database{db: db}

	byID, err := d.NotificationByID(ctx, "0123456789abcdef")
	if err != nil || byID == nil || byID.Recipient != "Support@Example.com" || strings.Join(byID.PublicIDs, ",") != "rpt_one,rpt_two" {
		t.Fatalf("by id: %+v err=%v", byID, err)
	}
	if missing, err := d.NotificationByID(ctx, "0000000000000000"); err != nil || missing != nil {
		t.Fatalf("unknown id: %+v err=%v", missing, err)
	}
	// Stored with and without angle brackets; quoted either way.
	for _, quoted := range []string{"relay-1@mail.gmail.com", "<relay-2@mail.gmail.com>"} {
		if found, err := d.NotificationByMessageID(ctx, []string{"unrelated@x", quoted}); err != nil || found == nil {
			t.Fatalf("by message id %q: %+v err=%v", quoted, found, err)
		}
	}
	if found, err := d.NotificationByMessageID(ctx, []string{"unrelated@x", " "}); err != nil || found != nil {
		t.Fatalf("unknown message id: %+v err=%v", found, err)
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	receivedAt := time.Date(2026, 11, 12, 8, 30, 0, 0, time.UTC)
	bounce := inbox.Record{MailboxMessageID: "gm-1", Result: "bounced", Notification: byID, PermanentBounce: true,
		Evidence: `{"kind":"bounce","status_code":"5.1.1","permanent":true}`, ReceivedAt: receivedAt}
	for i := 0; i < 2; i++ { // the second call is a repeat and must change nothing
		if err := d.RecordInboxMessage(ctx, bounce); err != nil {
			t.Fatal(err)
		}
	}
	if handled, err := d.InboxMessageHandled(ctx, "gm-1"); err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if handled, err := d.InboxMessageHandled(ctx, "gm-unknown"); err != nil || handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if n := count(`SELECT COUNT(*) FROM notify_outcomes WHERE outcome_type='bounced' AND source_type='inbox' AND source_ref='0123456789abcdef' AND endpoint_key='support@example.com' AND recorded_at='2026-11-12 08:30:00'`); n != 2 {
		t.Fatalf("one bounced row per report carried, once: %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM notify_outcomes WHERE JSON_UNQUOTE(JSON_EXTRACT(evidence_json,'$.status_code'))='5.1.1'`); n != 2 {
		t.Fatalf("evidence must be stored as JSON: %d", n)
	}
	memory, err := d.GetContactEndpointMemory(ctx, "email:support@example.com")
	if err != nil || memory == nil || memory.BounceCount != 1 || memory.LastResult != "bounced" || memory.CooldownUntil == nil ||
		!memory.CooldownUntil.Equal(receivedAt.Add(inboxPermanentBounceCooldown)) {
		t.Fatalf("memory=%+v err=%v", memory, err)
	}

	ack := inbox.Record{MailboxMessageID: "gm-2", Result: "acknowledged", Notification: byID,
		Evidence: `{"kind":"auto_ack","ticket_ref":"48213"}`, ReceivedAt: receivedAt.Add(time.Hour)}
	if err := d.RecordInboxMessage(ctx, ack); err != nil {
		t.Fatal(err)
	}
	memory, err = d.GetContactEndpointMemory(ctx, "email:support@example.com")
	if err != nil || memory.AckCount != 1 || memory.BounceCount != 1 || memory.LastResult != "acknowledged" ||
		!memory.CooldownUntil.Equal(receivedAt.Add(inboxPermanentBounceCooldown)) {
		t.Fatalf("an acknowledgement must not shorten a longer cool-down: %+v err=%v", memory, err)
	}
	timeline, err := d.ReportOutcomeTimeline(ctx, "rpt_one")
	if err != nil || len(timeline) != 2 || timeline[0].Type != "bounced" || timeline[1].Type != "acknowledged" || timeline[1].TicketRef != "48213" {
		t.Fatalf("public timeline=%+v err=%v", timeline, err)
	}

	optOut := inbox.Record{MailboxMessageID: "gm-3", Result: "unsubscribed", Notification: byID, OptOut: true, ReceivedAt: receivedAt}
	for i := 0; i < 2; i++ {
		if err := d.RecordInboxMessage(ctx, optOut); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(`SELECT COUNT(*) FROM opted_out_emails WHERE email='support@example.com'`); n != 1 {
		t.Fatalf("opt-out rows=%d", n)
	}
	if timeline, err := d.ReportOutcomeTimeline(ctx, "rpt_one"); err != nil || len(timeline) != 2 {
		t.Fatalf("an unsubscribe is not published: %+v err=%v", timeline, err)
	}

	before := count(`SELECT COUNT(*) FROM notify_outcomes`)
	for _, record := range []inbox.Record{
		{MailboxMessageID: "gm-4", Result: inbox.ResultUnmatched, ReceivedAt: receivedAt},
		{MailboxMessageID: "gm-5", Result: inbox.ResultUnparseable, ReceivedAt: receivedAt},
	} {
		if err := d.RecordInboxMessage(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if after := count(`SELECT COUNT(*) FROM notify_outcomes`); after != before {
		t.Fatalf("unmatched mail must record no outcome: %d -> %d", before, after)
	}
	if n := count(`SELECT COUNT(*) FROM inbox_messages WHERE notification_id IS NULL AND result IN ('unmatched','unparseable')`); n != 2 {
		t.Fatalf("unmatched mail is still remembered as handled: %d", n)
	}
}
