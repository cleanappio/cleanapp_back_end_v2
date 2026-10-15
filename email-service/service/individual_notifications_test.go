package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"email-service/config"
	"email-service/email"
	"email-service/models"

	"github.com/DATA-DOG/go-sqlmock"
)

type individualTestSender struct {
	to         []string
	fail       string
	beforeSend func(string)
}

func (s *individualTestSender) Provider() string { return "google_workspace" }
func (s *individualTestSender) SendEmailsWithAnalysis(to []string, _, _ []byte, _ *models.ReportAnalysis) error {
	if len(to) != 1 {
		panic("individual acceptance requires one recipient per transport call")
	}
	if s.beforeSend != nil {
		s.beforeSend(to[0])
	}
	s.to = append(s.to, to[0])
	if to[0] == s.fail {
		return errors.New("recipient rejected")
	}
	return nil
}
func (*individualTestSender) SendAggregateEmailsWithResults([]string, *models.BrandReportSummary, string) []email.CustomEmailSendResult {
	panic("unexpected aggregate send")
}
func (*individualTestSender) SendCustomEmails([]string, string, string, string) []email.CustomEmailSendResult {
	panic("unexpected custom send")
}

func expectIndividualPrior(mock sqlmock.Sqlmock, recipient string, count int) {
	mock.ExpectQuery(`(?s)SELECT COUNT\(DISTINCT seq\) FROM \(.*report_email_deliveries.*UNION ALL.*report_email_delivery_events`).WithArgs(recipient, 10, recipient, 10).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}
func expectIndividualOptOut(mock sqlmock.Sqlmock, recipient string, count int) {
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs(recipient).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}
func expectIndividualPublicPolicy(mock sqlmock.Sqlmock, publishable bool) {
	mock.ExpectQuery(`(?s)SELECT EXISTS\(SELECT 1 FROM reports r.*ra.language='en'.*WHERE r.seq=\? AND ra.is_valid=TRUE.*visibility='public'.*NOT EXISTS.*report_status.*NOT EXISTS.*reports_owners`).WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"publishable"}).AddRow(publishable))
}
func expectIndividualReceipt(mock sqlmock.Sqlmock, recipient string) {
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT IGNORE INTO report_email_delivery_events.*SELECT old.seq`).WithArgs(10, recipient).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO report_email_delivery_events.*provider_message_id.*VALUES`).WithArgs(10, recipient, "area_contact", "google_workspace", "", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)INSERT INTO report_email_deliveries.*ON DUPLICATE KEY UPDATE`).WithArgs(10, recipient, "area_contact", "google_workspace", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}
func expectIndividualRetry(mock sqlmock.Sqlmock, reason string) {
	mock.ExpectExec(`INSERT INTO email_report_retry`).WithArgs(10, reason, sqlmock.AnyArg(), retryReasonNoValidEmails, 6, 5, 168, retryReasonNoValidEmails, 10).WillReturnResult(sqlmock.NewResult(1, 1))
}
func individualFixture(t *testing.T) (*EmailService, sqlmock.Sqlmock, *individualTestSender) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sender := &individualTestSender{}
	return &EmailService{db: db, email: sender, config: &config.Config{}}, mock, sender
}
func individualReport() models.Report { return models.Report{Seq: 10, Image: []byte{1}} }

func TestIndividualPartialSendPersistsAcceptanceBeforeNextRecipient(t *testing.T) {
	s, mock, sender := individualFixture(t)
	sender.fail = "b@city.org"
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	expectIndividualPublicPolicy(mock, true)
	expectReceiptPreflight(mock)
	expectIndividualReceipt(mock, "a@city.org")
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("a@city.org").WillReturnResult(sqlmock.NewResult(1, 1))
	expectIndividualPrior(mock, "b@city.org", 0)
	expectIndividualOptOut(mock, "b@city.org", 0)
	sender.beforeSend = func(recipient string) {
		if recipient == "b@city.org" {
			// No later expectations exist yet: this proves the first recipient's
			// acceptance committed before attempting the rejected recipient.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("next send preceded receipt persistence: %v", err)
			}
			expectIndividualRetry(mock, retryReasonAggregateFailure)
		}
	}
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{" A@CITY.ORG ", "a@city.org", "b@city.org"}, nil, "area_contact", "")
	if err == nil || out.Complete || out.Accepted != 1 || out.Failed != 1 || out.Eligible != 2 || out.RetryReason != retryReasonAggregateFailure || len(sender.to) != 2 {
		t.Fatalf("partial result lost or completed: %#v err=%v sends=%v", out, err, sender.to)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualRetrySkipsPreviouslyAcceptedRecipient(t *testing.T) {
	s, mock, sender := individualFixture(t)
	expectIndividualPrior(mock, "a@city.org", 1)
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org", "A@city.org"}, nil, "area_contact", "")
	if err != nil || !out.Complete || out.AlreadyAccepted != 1 || out.Accepted != 0 || len(sender.to) != 0 {
		t.Fatalf("accepted recipient was resent or lost: %#v err=%v sends=%v", out, err, sender.to)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualThrottleErrorFailsClosedAndKeepsReportPending(t *testing.T) {
	s, mock, sender := individualFixture(t)
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	mock.ExpectQuery(`SELECT last_sent_at FROM brand_email_throttle`).WithArgs("acme", "a@city.org").WillReturnError(errors.New("throttle database unavailable"))
	expectIndividualRetry(mock, retryReasonThrottleError)
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org"}, nil, "inferred_contact", " ACME ")
	if err == nil || out.Complete || out.Held != 1 || out.Accepted != 0 || len(sender.to) != 0 {
		t.Fatalf("throttle error permitted a send or completed report: %#v err=%v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualThrottledRecipientRetainsCooldownAndNoSend(t *testing.T) {
	s, mock, sender := individualFixture(t)
	lastSent := time.Now().UTC()
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	mock.ExpectQuery(`SELECT last_sent_at FROM brand_email_throttle`).WithArgs("acme", "a@city.org").WillReturnRows(sqlmock.NewRows([]string{"last_sent_at"}).AddRow(lastSent))
	expectIndividualRetry(mock, retryReasonAllThrottled)
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org"}, nil, "inferred_contact", "acme")
	if err == nil || out.Complete || out.Held != 1 || out.RetryAt == nil || !out.RetryAt.Equal(lastSent.Add(7*24*time.Hour)) || len(sender.to) != 0 {
		t.Fatalf("cooldown did not preserve pending report: %#v err=%v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualOptOutAndEmptyRecipientsNeverCompleteOrCreateReceipts(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{true: "empty", false: "opted_out"}[empty], func(t *testing.T) {
			s, mock, sender := individualFixture(t)
			recipients := []string{"a@city.org"}
			if empty {
				recipients = nil
			} else {
				expectIndividualPrior(mock, "a@city.org", 0)
				expectIndividualOptOut(mock, "a@city.org", 1)
			}
			expectIndividualRetry(mock, retryReasonNoValidEmails)
			out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, recipients, nil, "area_contact", "")
			if err == nil || out.Complete || out.Accepted != 0 || len(sender.to) != 0 {
				t.Fatalf("no send completed report: %#v err=%v", out, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIndividualReceiptFailureQuarantinesBeforeAnyNextSend(t *testing.T) {
	s, mock, sender := individualFixture(t)
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	expectIndividualPublicPolicy(mock, true)
	expectReceiptPreflight(mock)
	for i := 0; i < 3; i++ {
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT IGNORE INTO report_email_delivery_events`).WillReturnError(errors.New("write unavailable"))
		mock.ExpectRollback()
	}
	expectIndividualRetry(mock, retryReasonReceiptPending)
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org", "b@city.org"}, nil, "area_contact", "")
	if err == nil || !strings.Contains(err.Error(), "quarantined") || out.Complete || out.Accepted != 1 || len(sender.to) != 1 || out.RetryReason != retryReasonReceiptPending {
		t.Fatalf("receipt failure lost accepted send or permitted next one: %#v err=%v sends=%v", out, err, sender.to)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualDryRunIsReadOnlyAndNeverCompletes(t *testing.T) {
	s, mock, sender := individualFixture(t)
	s.config.DryRun = true
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org"}, nil, "area_contact", "acme")
	if err != nil || out.Complete || out.Accepted != 0 || out.AlreadyAccepted != 0 || len(sender.to) != 0 {
		t.Fatalf("dry run fabricated progress: %#v err=%v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A successful brand-throttled send must also persist its acceptance before
// either secondary history table is updated.
func TestIndividualAcceptedSendCompletesOnlyAfterReceipt(t *testing.T) {
	s, mock, sender := individualFixture(t)
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	mock.ExpectQuery(`SELECT last_sent_at FROM brand_email_throttle`).WithArgs("acme", "a@city.org").WillReturnError(sql.ErrNoRows)
	expectIndividualPublicPolicy(mock, true)
	expectReceiptPreflight(mock)
	expectIndividualReceipt(mock, "a@city.org")
	mock.ExpectExec(`INSERT INTO brand_email_throttle`).WithArgs("acme", "a@city.org").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("a@city.org").WillReturnResult(sqlmock.NewResult(1, 1))
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org"}, nil, "area_contact", "acme")
	if err != nil || !out.Complete || out.Accepted != 1 || len(sender.to) != 1 || out.RetryAt != nil {
		t.Fatalf("accepted send did not complete correctly: %#v err=%v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIndividualPrivateOrInvalidReportCannotSendOrComplete(t *testing.T) {
	s, mock, sender := individualFixture(t)
	expectIndividualPrior(mock, "a@city.org", 0)
	expectIndividualOptOut(mock, "a@city.org", 0)
	expectIndividualPublicPolicy(mock, false)
	expectIndividualRetry(mock, retryReasonIndividualPublicPolicy)
	out, err := s.sendIndividualReportNotifications(context.Background(), individualReport(), &models.ReportAnalysis{}, []string{"a@city.org"}, nil, "area_contact", "")
	if err == nil || out.Complete || out.Accepted != 0 || out.Held != 1 || len(sender.to) != 0 {
		t.Fatalf("private/invalid report produced notification or completion: %#v err=%v", out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
