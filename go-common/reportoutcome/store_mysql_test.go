package reportoutcome

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func outcomeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CLEANAPP_OUTCOME_TEST_DSN")
	if dsn == "" {
		t.Skip("dedicated outcome MySQL fixture not configured")
	}
	if os.Getenv("CLEANAPP_OUTCOME_TEST_ALLOW_SCHEMA") != "1" {
		t.Fatal("fixture schema changes require explicit opt-in")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil || !regexp.MustCompile(`^cleanapp_outcome_test(?:_[A-Za-z0-9]+)?$`).MatchString(config.DBName) {
		t.Fatal("refusing schema changes outside an explicitly named outcome test database")
	}
	config.ParseTime = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal("cannot open dedicated test database")
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Ping(); err != nil {
		t.Fatal("dedicated outcome test database unavailable")
	}
	tables := []string{"report_outcomes", "wire_submissions_raw", "wire_evidence_projection", "report_evidence_source_projection", "report_analysis", "report_evidence", "report_side_effect_policy", "report_email_delivery_events", "report_email_deliveries", "email_report_retry", "sent_reports_emails", "epc_outbox", "reports_owners", "report_status", "report_raw", "reports"}
	for _, table := range tables {
		if _, err = db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatal(err)
		}
	}
	statements := []string{
		`CREATE TABLE reports(seq INT PRIMARY KEY,public_id VARCHAR(64) UNIQUE,description TEXT,image BLOB)`,
		`CREATE TABLE report_raw(report_seq INT PRIMARY KEY,fetcher_id VARCHAR(64),visibility VARCHAR(16),trust_level VARCHAR(16),promoted_to_public_at TIMESTAMP NULL,analysed_published_at TIMESTAMP NULL)`,
		`CREATE TABLE report_status(seq INT PRIMARY KEY,status VARCHAR(16))`,
		`CREATE TABLE reports_owners(seq INT,owner VARCHAR(128),is_public BOOLEAN,KEY(seq))`,
		`CREATE TABLE wire_submissions_raw(submission_id VARCHAR(64) PRIMARY KEY,report_seq INT,fetcher_id VARCHAR(64),actor_kind VARCHAR(16),channel VARCHAR(32),agent_id VARCHAR(128),agent_json JSON,provenance_json JSON,report_json JSON,submitted_at TIMESTAMP(6),KEY(report_seq))`,
		`CREATE TABLE wire_evidence_projection(report_seq INT,contains_pii BOOLEAN,requires_redaction BOOLEAN,KEY(report_seq))`,
		`CREATE TABLE report_evidence_source_projection(seq INT PRIMARY KEY,privacy_restricted BOOLEAN)`,
		`CREATE TABLE report_analysis(seq INT,source VARCHAR(64),language VARCHAR(8),is_valid BOOLEAN,classification VARCHAR(16),title TEXT,description TEXT,summary TEXT,brand_name VARCHAR(255),brand_display_name VARCHAR(255),analysis_image BLOB,created_at TIMESTAMP(6),updated_at TIMESTAMP(6),PRIMARY KEY(seq,source,language))`,
		`CREATE TABLE report_evidence(seq INT PRIMARY KEY,report_kind VARCHAR(16),entity_relationship VARCHAR(16),evidence_version INT,evidence_excerpt TEXT,reported_impact TEXT,classification_reason TEXT)`,
		`CREATE TABLE report_side_effect_policy(report_seq INT PRIMARY KEY,source_namespace VARCHAR(64),reason VARCHAR(255),policy_version INT,suppress_email BOOLEAN,suppress_notifications BOOLEAN,suppress_rewards BOOLEAN,suppress_routing BOOLEAN,suppress_analysis BOOLEAN)`,
		`CREATE TABLE report_email_delivery_events(seq INT,recipient_email VARCHAR(255),delivery_source VARCHAR(64),delivery_status VARCHAR(16),sent_at TIMESTAMP NULL,KEY(seq))`,
		`CREATE TABLE report_email_deliveries(seq INT,recipient_email VARCHAR(255),delivery_source VARCHAR(64),delivery_status VARCHAR(16),sent_at TIMESTAMP NULL,KEY(seq))`,
		`CREATE TABLE email_report_retry(seq INT PRIMARY KEY,reason VARCHAR(64),next_attempt_at TIMESTAMP NULL,retry_count INT DEFAULT 1)`,
		`CREATE TABLE sent_reports_emails(seq INT PRIMARY KEY)`,
		`CREATE TABLE epc_outbox(event_key VARCHAR(191) PRIMARY KEY,status VARCHAR(16),blockscan_message_id VARCHAR(128),sent_at TIMESTAMP NULL,next_attempt_at DATETIME NULL)`,
	}
	for _, statement := range statements {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(context.Background(), db); err != nil {
		t.Fatal("outcome migration is not idempotent:", err)
	}
	return db
}

func TestMySQLStoreOutcomeLifecycle(t *testing.T) {
	db := outcomeTestDB(t)
	ctx := context.Background()
	store := NewStore(db)
	created := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(seq int, handling *Handling, description, classification, brand string, analyzed bool) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `INSERT INTO reports VALUES(?,?,?,NULL)`, seq, fmt.Sprintf("rpt_fixture_%03d", seq), description); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO report_raw(report_seq,fetcher_id,visibility,trust_level) VALUES(?,'public-agent-anonymous-v1','shadow','unverified')`, seq); err != nil {
			t.Fatal(err)
		}
		report, _ := json.Marshal(map[string]any{"description": description, "domain": classification, "digital_context": map[string]any{"url": "https://example.invalid/app", "product_or_site": "Example App"}})
		if _, err = tx.ExecContext(ctx, `INSERT INTO wire_submissions_raw VALUES(?,?,'public-agent-anonymous-v1','machine','public_agent','public-agent-anonymous-v1','{"agent_type":"public_agent"}','{"generation_method":"agent_submission"}',?,NOW(6))`, fmt.Sprintf("sub_%d", seq), seq, report); err != nil {
			t.Fatal(err)
		}
		if err = IntakeRecordTx(ctx, tx, seq, handling); err != nil {
			t.Fatal(err)
		}
		permission, _ := NormalizeHandling(handling)
		blocked := permission.Notices != "automatic"
		if _, err = tx.ExecContext(ctx, `INSERT INTO report_side_effect_policy VALUES(?,'public-agent','Anonymous agent intake is private pending review',1,?,?,TRUE,?,FALSE)`, seq, blocked, blocked, blocked); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if analyzed {
			exec(`INSERT INTO report_analysis VALUES(?,'fixture','en',TRUE,?,'Save returns error',?,?,?,'',NULL,?,?)`, seq, classification, description, description, brand, created, created)
			exec(`INSERT INTO report_evidence VALUES(?,'firsthand','affected',1,?,'','Source observation')`, seq, description)
		}
	}
	automatic := &Handling{Publication: "automatic", Notices: "automatic"}
	publicationOnly := &Handling{Publication: "automatic", Notices: "none"}
	complete := func(seq int) Record {
		t.Helper()
		record, err := store.Complete(ctx, seq, Analysis{Available: true})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	get := func(seq int) *Outcome {
		t.Helper()
		outcome, err := store.Get(ctx, seq)
		if err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	seed(1, automatic, "The Save button returns error 500 after login.", "digital", "", true)
	record := complete(1)
	if !record.PublishPending || !record.ServerAuthorizedPublication || record.Outcome.Publication.Status != "public" || record.Outcome.Final {
		t.Fatalf("new public projection lost its durable pending event: %+v", record)
	}
	if err := store.MarkPublished(ctx, 1); err != nil {
		t.Fatal(err)
	}
	outcome := get(1)
	if outcome.Final || outcome.Delivery[0].Status != "pending" || outcome.Delivery[1].ReasonCode != "no_eligible_blockscan_subject" {
		t.Fatalf("unassessed routing was falsely final: %+v", outcome)
	}
	exec(`INSERT INTO email_report_retry VALUES(1,'no_valid_emails',DATE_ADD(NOW(),INTERVAL 60 SECOND),1)`)
	if outcome = get(1); !outcome.Final || outcome.Delivery[0].ReasonCode != "no_verified_recipient" {
		t.Fatalf("producer assessment did not complete outcome: %+v", outcome)
	}
	exec(`INSERT INTO report_email_delivery_events VALUES(1,'support@example.invalid','inferred_contact','sent',NULL)`)
	if outcome = get(1); outcome.Delivery[0].Status != "uncertain" {
		t.Fatal("missing provider receipt time was called sent")
	}
	exec(`UPDATE report_email_delivery_events SET sent_at=? WHERE seq=1`, created)
	if outcome = get(1); outcome.Delivery[0].Status != "sent" || outcome.Delivery[0].LastSentAt == nil {
		t.Fatal("real timestamped provider evidence was lost")
	}
	exec(`INSERT INTO epc_outbox VALUES('legacy-dev:report:1','sent','999',NOW(),NULL)`)
	if outcome = get(1); outcome.Delivery[1].EvidenceCount != 0 {
		t.Fatal("legacy numeric sequence was mistaken for a production receipt")
	}
	seed(2, publicationOnly, "The Save button returns error 500 after login.", "digital", "", true)
	if record = complete(2); record.Outcome.Decision.State != ClusterCandidate || record.Outcome.Relationship == nil || record.Outcome.Relationship.Candidates[0].Reference != "rpt_fixture_001" {
		t.Fatalf("repeated observation was not additive evidence: %+v", record)
	}
	seed(3, publicationOnly, "After login Save returns error 500.", "digital", "", true)
	if record = complete(3); record.Outcome.Decision.State != ClusterCandidate {
		t.Fatal("similar reordered observation did not become a cluster candidate")
	}
	var ownRecords int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reports WHERE seq IN (1,2,3)`).Scan(&ownRecords); err != nil || ownRecords != 3 {
		t.Fatal("cluster candidacy removed independent reports")
	}
	exec(`UPDATE report_raw SET visibility='shadow' WHERE report_seq=1`)
	if outcome = get(2); outcome.Relationship == nil || len(outcome.Relationship.Candidates) != 0 {
		t.Fatal("a newly private candidate remained visible")
	}
	seed(4, publicationOnly, "A bridge joint has broken at the marked location.", "physical", "", true)
	complete(4)
	if err := store.MarkPublished(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if outcome = get(4); outcome.Publication.CanonicalPath != "/physical/report/rpt_fixture_004" {
		t.Fatal("physical publication received a digital canonical path")
	}
	seed(5, publicationOnly, "The Save button fails on example.invalid.", "digital", "", true)
	exec(`DELETE FROM report_side_effect_policy WHERE report_seq=5`)
	if record = complete(5); record.ServerAuthorizedPublication || record.Outcome.Publication.ReasonCode != "explicit_effect_policy_missing" {
		t.Fatal("missing consent policy authorized publication/rewards")
	}
	seed(6, publicationOnly, "Checkout fails on example.invalid.", "digital", "", true)
	exec(`INSERT INTO report_analysis SELECT seq,source,'fr',is_valid,classification,title,'fixture@example.invalid',summary,brand_name,brand_display_name,analysis_image,created_at,updated_at FROM report_analysis WHERE seq=6 AND language='en'`)
	if record = complete(6); record.ServerAuthorizedPublication || record.Outcome.Publication.Status != "held" {
		t.Fatal("private translated public field bypassed authorization")
	}
	seed(7, nil, "The app returns an error on save.", "digital", "", true)
	record = complete(7)
	if !record.Outcome.Final || record.Outcome.Publication.Status != "private" {
		t.Fatal("old private handling was upgraded")
	}
	if record.Outcome.Processing.CompletedAt == nil || !record.Outcome.Processing.CompletedAt.Equal(created) {
		t.Fatal("stored analysis completion time was invented")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = IntakeRecordTx(ctx, tx, 7, automatic); err == nil {
		tx.Rollback()
		t.Fatal("retry upgraded original private authorization")
	}
	tx.Rollback()
	seed(8, publicationOnly, "Checkout fails on example.invalid.", "digital", "", false)
	exec(`UPDATE wire_submissions_raw SET report_json=JSON_SET(report_json,'$.digital_context.password','fixture-private-value') WHERE report_seq=8`)
	record, err = store.Complete(ctx, 8, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome.Processing.Status != "not_required" || record.Outcome.Decision.State != Held || !record.Outcome.Final {
		t.Fatal("privacy preflight either called analysis complete or stayed pending")
	}
	seed(9, publicationOnly, "An external source documents a checkout failure on Example App.", "digital", "", true)
	exec(`UPDATE report_evidence SET report_kind='coverage' WHERE seq=9`)
	if record = complete(9); !record.ServerAuthorizedPublication || (record.Outcome.Decision.State != Accepted && record.Outcome.Decision.State != ClusterCandidate) {
		t.Fatal("ordinary valid sourced coverage was blanket-held")
	}
	seed(10, publicationOnly, "The button failed.", "digital", "", true)
	exec(`UPDATE wire_submissions_raw SET report_json=JSON_SET(report_json,'$.digital_context',JSON_OBJECT()) WHERE report_seq=10`)
	if record = complete(10); record.Outcome.Decision.State != NeedsInformation || record.ServerAuthorizedPublication {
		t.Fatal("missing affected context authorized an automatic action")
	}
	seed(12, publicationOnly, "The upload fails on the source application.", "digital", "", true)
	exec(`DELETE FROM report_evidence WHERE seq=12`)
	transient := get(12)
	if transient.Processing.Status != "analyzed" || transient.Processing.CompletedAt == nil || !transient.Processing.CompletedAt.Equal(created) ||
		transient.Decision == nil || transient.Decision.State != Pending || transient.Final || transient.NextAction.Type != "wait" {
		t.Fatal("saved English produced a terminal decision before durable completion")
	}
	var pendingDecision string
	if err := db.QueryRow(`SELECT decision_state FROM report_outcomes WHERE report_seq=12`).Scan(&pendingDecision); err != nil || pendingDecision != "pending" {
		t.Fatal("status projection mutated the pending decision")
	}
	exec(`INSERT INTO report_evidence VALUES(12,'firsthand','affected',1,'The upload fails on the source application.','','Source observation')`)
	if record = complete(12); record.Outcome.Decision.State == Pending {
		t.Fatal("durable completion did not finish the pending review")
	}
	// Recovery leaves the unexamined due tail available when a small first batch fails.
	for seq := 20; seq < 27; seq++ {
		seed(seq, nil, "The save button failed on example.invalid.", "digital", "", true)
	}
	exec(`UPDATE report_outcomes SET next_reconcile_at=DATE_ADD(NOW(6),INTERVAL 1 HOUR)`)
	exec(`UPDATE report_outcomes SET next_reconcile_at='2000-01-01' WHERE report_seq BETWEEN 20 AND 26`)
	first, err := store.Pending(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Pending(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 || first[0] == second[0] || first[1] == second[1] {
		t.Fatalf("failed first batch starved ready tail: %v then %v", first, second)
	}
	seed(30, publicationOnly, "The export action fails on example.invalid.", "digital", "", true)
	complete(30)
	for attempt := 0; attempt < 5; attempt++ {
		if err = store.MarkPublicationFailed(ctx, 30, "fixture_failure"); err != nil {
			t.Fatal(err)
		}
	}
	if pending, err := store.PublicationPending(ctx, 30); err != nil || pending {
		t.Fatal("publication retries did not stop at their durable attempt ceiling")
	}
	var state string
	var reconciling bool
	if err = db.QueryRow(`SELECT public_event_status,reconcile_pending FROM report_outcomes WHERE report_seq=30`).Scan(&state, &reconciling); err != nil || state != "failed" || reconciling {
		t.Fatal("terminal failed event kept scanning/retrying")
	}
}
