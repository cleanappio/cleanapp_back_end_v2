package service

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"email-service/config"
	"email-service/email"
	"email-service/models"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestTruncatedBatchCannotAttributeSendToPartialReportID(t *testing.T) {
	if got := parseCompleteReportSeqs("1344678:,1280207:,1280207:,123"); !reflect.DeepEqual(got, []int64{1344678, 1280207}) {
		t.Fatalf("partial/duplicate ID accepted: %v", got)
	}
}

func TestAggregateFindingsOnlyLoadPendingPublicValidReportsAndThreeImages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := sqlmock.NewRows([]string{"seq", "public_id", "title", "summary", "excerpt", "impact", "kind", "classification", "ts"})
	for i := 4; i >= 1; i-- {
		rows.AddRow(i, "rpt_safe", "Issue", "Generated summary", "Original wording", "Blocked", "firsthand", "digital", time.Now())
	}
	mock.ExpectQuery(`(?s)WHERE r.seq IN \(\?,\?,\?,\?\) AND ra.brand_name=\? AND ra.is_valid=TRUE.*visibility='public'.*NOT EXISTS.*report_status.*NOT EXISTS.*reports_owners.*ORDER BY r.ts DESC,r.seq DESC LIMIT 20`).WithArgs(4, 3, 2, 1, "acme").WillReturnRows(rows)
	for _, seq := range []int64{4, 3, 2} {
		mock.ExpectQuery(`(?s)SELECT CASE WHEN OCTET_LENGTH\(r.image\)<=\? THEN r.image ELSE NULL END FROM reports r.*WHERE r.seq=\?.*NOT EXISTS.*report_status.*NOT EXISTS.*reports_owners`).WithArgs(email.MaxPreviewInputBytes, seq).WillReturnRows(sqlmock.NewRows([]string{"image"}).AddRow(nil))
	}
	reports, err := (&EmailService{db: db}).getAggregateFindings(context.Background(), &models.BrandReportSummary{BrandName: "acme", ReportSeqs: []int64{4, 3, 2, 1}})
	if err != nil || len(reports) != 4 || reports[0].Seq != 4 || reports[0].OriginalExcerpt != "Original wording" || reports[0].Summary != "Generated summary" {
		t.Fatalf("wrong pending findings: %#v %v", reports, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectAcceptedEvent(mock sqlmock.Sqlmock, seq int64, recipient, source string, at time.Time) {
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT IGNORE INTO report_email_delivery_events.*SELECT old.seq.*NOT EXISTS`).WithArgs(seq, recipient).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO report_email_delivery_events.*provider_message_id.*VALUES`).WithArgs(seq, recipient, source, "google_workspace", "accepted", at, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec(`(?s)INSERT INTO report_email_deliveries.*ON DUPLICATE KEY UPDATE`).WithArgs(seq, recipient, source, "google_workspace", at).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

func expectReceiptPreflight(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO report_email_delivery_events.*WHERE FALSE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)INSERT INTO report_email_deliveries.*WHERE FALSE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
}

func TestDeliveryEventsPreserveOldLedgerAndAppendAcceptanceAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &EmailService{db: db}
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	expectAcceptedEvent(mock, 10, "support@company.com", "aggregate_report", at)
	if err := s.recordReportEmailDeliveryResult(context.Background(), 10, " Support@Company.com ", "aggregate_report", email.CustomEmailSendResult{Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted", SentAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.recordReportEmailDeliveryResult(context.Background(), 10, "support@company.com", "aggregate_report", email.CustomEmailSendResult{Status: "failed"}); err == nil {
		t.Fatal("failed send created delivery event")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryEventFailureDoesNotOverwritePriorLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT IGNORE INTO report_email_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO report_email_delivery_events`).WillReturnError(errors.New("database write unavailable"))
	mock.ExpectRollback()
	err = (&EmailService{db: db}).recordReportEmailDeliveryAttempt(context.Background(), 10, "a@company.com", "aggregate_report", email.CustomEmailSendResult{Status: "sent", Provider: "google_workspace", SentAt: time.Now()})
	if err == nil {
		t.Fatal("append error lost")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateDeliverySourceOnlyClaimsReportsActuallyLinked(t *testing.T) {
	reports := make([]models.BrandReportFinding, 21)
	for i := range reports {
		reports[i] = models.BrandReportFinding{Seq: int64(i + 1), PublicID: "rpt_safe", Classification: "digital"}
	}
	if aggregateDeliverySource(1, reports) != "aggregate_report" || aggregateDeliverySource(20, reports) != "aggregate_report" || aggregateDeliverySource(21, reports) != "aggregate_brand" || aggregateDeliverySource(100, reports) != "aggregate_brand" {
		t.Fatal("digest-only report mislabeled as linked")
	}
}

type aggregateTestSender struct {
	recipients []string
	at         time.Time
	fail       bool
}

func (s *aggregateTestSender) Provider() string { return "google_workspace" }
func (s *aggregateTestSender) SendAggregateEmailsWithResults(to []string, summary *models.BrandReportSummary, _ string) []email.CustomEmailSendResult {
	s.recipients = append(s.recipients, to...)
	r := email.CustomEmailSendResult{Email: to[0], Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted", SentAt: s.at}
	if s.fail || to[0] == "b@company.com" {
		r.Status = "failed"
		r.Error = "rejected"
		r.ProviderMessageID = ""
		r.SentAt = time.Time{}
	}
	return []email.CustomEmailSendResult{r}
}
func (s *aggregateTestSender) SendEmailsWithAnalysis([]string, []byte, []byte, *models.ReportAnalysis) error {
	return errors.New("unexpected individual send")
}
func (s *aggregateTestSender) SendCustomEmails([]string, string, string, string) []email.CustomEmailSendResult {
	panic("unexpected custom send")
}

func expectAggregateBatch(mock sqlmock.Sqlmock, recipients string) {
	mock.ExpectQuery(`(?s)GROUP BY ra.brand_name, ra.brand_display_name`).WillReturnRows(sqlmock.NewRows([]string{"brand", "display", "new", "total", "seqs", "latest", "classification", "emails"}).AddRow("acme", "Acme", 1, 4, "10:", 10, "digital", recipients))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM brand_email_throttle`).WithArgs("acme").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}
func expectSendableRecipient(mock sqlmock.Sqlmock, recipient string) {
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT seq\) FROM report_email_deliveries`).WithArgs(recipient, 10).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs(recipient).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT last_sent_at FROM brand_email_throttle`).WithArgs("acme", recipient).WillReturnError(sql.ErrNoRows)
}
func expectOneFinding(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`(?s)SELECT r.seq,r.public_id.*ORDER BY r.ts DESC,r.seq DESC LIMIT 20`).WithArgs(10, "acme").WillReturnRows(sqlmock.NewRows([]string{"seq", "public_id", "title", "summary", "excerpt", "impact", "kind", "classification", "ts"}).AddRow(10, "rpt_safe", "Issue", "Summary", "Original", "Impact", "firsthand", "digital", time.Now()))
	mock.ExpectQuery(`SELECT CASE WHEN OCTET_LENGTH`).WithArgs(email.MaxPreviewInputBytes, 10).WillReturnRows(sqlmock.NewRows([]string{"image"}).AddRow(nil))
}
func expectAggregateRetry(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`INSERT INTO email_report_retry`).WithArgs(10, retryReasonAggregateFailure, sqlmock.AnyArg(), retryReasonNoValidEmails, 6, 5, 168, retryReasonNoValidEmails, 10).WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestAggregatePartialSendRecordsOnlyAcceptedRecipientAndRetriesFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	sender := &aggregateTestSender{at: at}
	expectAggregateBatch(mock, "a@company.com,b@company.com")
	expectReceiptPreflight(mock)
	expectSendableRecipient(mock, "a@company.com")
	expectSendableRecipient(mock, "b@company.com")
	expectOneFinding(mock)
	mock.ExpectExec(`INSERT INTO brand_email_throttle`).WithArgs("acme", "a@company.com").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("a@company.com").WillReturnResult(sqlmock.NewResult(1, 1))
	expectAcceptedEvent(mock, 10, "a@company.com", "aggregate_report", at)
	expectAggregateRetry(mock)
	s := &EmailService{db: db, email: sender, config: &config.Config{MaxDailyEmailsPerBrand: 10}}
	if err := s.ProcessBrandNotifications(); err != nil {
		t.Fatal(err)
	}
	if len(sender.recipients) != 2 {
		t.Fatalf("wrong send count: %v", sender.recipients)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateDryRunCreatesNoAcceptanceEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sender := &aggregateTestSender{}
	expectAggregateBatch(mock, "a@company.com")
	expectSendableRecipient(mock, "a@company.com")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO sent_reports_emails (seq) VALUES (?)")).WithArgs(10).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM email_report_retry WHERE seq = ?")).WithArgs(10).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := (&EmailService{db: db, email: sender, config: &config.Config{DryRun: true, MaxDailyEmailsPerBrand: 10}}).ProcessBrandNotifications(); err != nil {
		t.Fatal(err)
	}
	if len(sender.recipients) != 0 {
		t.Fatal("dry run attempted email")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptedReceiptFailureQuarantinesBatchAndStopsExternalSends(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sender := &aggregateTestSender{at: time.Now()}
	expectAggregateBatch(mock, "a@company.com,b@company.com")
	expectReceiptPreflight(mock)
	expectSendableRecipient(mock, "a@company.com")
	expectSendableRecipient(mock, "b@company.com")
	expectOneFinding(mock)
	mock.ExpectExec(`INSERT INTO brand_email_throttle`).WithArgs("acme", "a@company.com").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("a@company.com").WillReturnResult(sqlmock.NewResult(1, 1))
	for i := 0; i < 3; i++ {
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT IGNORE INTO report_email_delivery_events`).WillReturnError(errors.New("write unavailable"))
		mock.ExpectRollback()
	}
	mock.ExpectExec(`INSERT INTO email_report_retry`).WithArgs(10, retryReasonReceiptPending, sqlmock.AnyArg(), retryReasonNoValidEmails, 6, 5, 168, retryReasonNoValidEmails, 10).WillReturnResult(sqlmock.NewResult(1, 1))
	err = (&EmailService{db: db, email: sender, config: &config.Config{MaxDailyEmailsPerBrand: 10}}).ProcessBrandNotifications()
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("receipt failure not surfaced: %v", err)
	}
	if len(sender.recipients) != 1 || sender.recipients[0] != "a@company.com" {
		t.Fatalf("external sends continued after receipt failure: %v", sender.recipients)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
