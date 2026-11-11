package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cleanapp-common/mailreply"
	"email-service/config"
	"email-service/email"
	"email-service/models"

	"github.com/DATA-DOG/go-sqlmock"
)

// Built at run time so secret scanners do not mistake it for a credential.
var testReplySecret = strings.Repeat("k", 32)

func TestReplyTrackingOffLeavesNotificationUntouched(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &EmailService{db: db, config: &config.Config{ReplyMailbox: "info@cleanapp.io", ReplyTokenSecret: testReplySecret}}
	summary := models.BrandReportSummary{}
	id := s.prepareReplyTracking(&summary)
	if id != "" || summary.ReplyTo != "" {
		t.Fatalf("tracking off must not add a reply address: id=%q replyTo=%q", id, summary.ReplyTo)
	}
	// No ID means no writes at all: sqlmock fails on any unexpected statement.
	s.recordNotificationMessage(context.Background(), id, "xai", "support@x.ai", email.CustomEmailSendResult{Status: "sent", SentAt: time.Now()}, []int64{1})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReplyTrackingSignsAddressAndRecordsCarriedReports(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &EmailService{db: db, config: &config.Config{ReplyTrackingEnabled: true, ReplyMailbox: "info@cleanapp.io", ReplyTokenSecret: testReplySecret}}
	summary := models.BrandReportSummary{}
	id := s.prepareReplyTracking(&summary)
	if len(id) != 16 {
		t.Fatalf("notification ID must be 16 characters, got %q", id)
	}
	parsed, ok := mailreply.ParseAddress(summary.ReplyTo, []byte(testReplySecret))
	if !ok || parsed != id {
		t.Fatalf("reply address %q does not carry ID %q", summary.ReplyTo, id)
	}
	if _, ok := mailreply.ParseAddress(summary.ReplyTo, []byte(strings.Repeat("x", 32))); ok {
		t.Fatal("reply address verified under the wrong secret")
	}
	other := models.BrandReportSummary{}
	if s.prepareReplyTracking(&other) == id {
		t.Fatal("two notifications received the same ID")
	}

	sentAt := time.Now().UTC()
	result := email.CustomEmailSendResult{Status: "sent", ProviderMessageID: "<abc@cleanapp.io>", SentAt: sentAt}
	mock.ExpectExec(`INSERT INTO notification_messages`).WithArgs(id, "<abc@cleanapp.io>", "support@x.ai", "xai", sentAt).WillReturnResult(sqlmock.NewResult(1, 1))
	for _, seq := range []int64{4, 2} {
		mock.ExpectExec(`INSERT IGNORE INTO notification_message_reports`).WithArgs(id, seq).WillReturnResult(sqlmock.NewResult(1, 1))
	}
	s.recordNotificationMessage(context.Background(), id, "xai", "support@x.ai", result, []int64{4, 2})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReplyTrackingNeverRecordsUnacceptedSendAndSurvivesWriteFailure(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &EmailService{db: db, config: &config.Config{ReplyTrackingEnabled: true, ReplyMailbox: "info@cleanapp.io", ReplyTokenSecret: testReplySecret}}
	s.recordNotificationMessage(context.Background(), "0123456789abcdef", "xai", "support@x.ai", email.CustomEmailSendResult{Status: "failed"}, []int64{1})

	// A failed header insert stops there: no orphan report rows, no panic.
	mock.ExpectExec(`INSERT INTO notification_messages`).WillReturnError(errors.New("db down"))
	s.recordNotificationMessage(context.Background(), "0123456789abcdef", "xai", "support@x.ai", email.CustomEmailSendResult{Status: "sent", SentAt: time.Now()}, []int64{1})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReplyTrackingWithUnusableSecretSendsWithoutReplyAddress(t *testing.T) {
	s := &EmailService{config: &config.Config{ReplyTrackingEnabled: true, ReplyMailbox: "info@cleanapp.io", ReplyTokenSecret: "short"}}
	summary := models.BrandReportSummary{}
	if id := s.prepareReplyTracking(&summary); id != "" || summary.ReplyTo != "" {
		t.Fatalf("unusable secret must disable tracking for the send: id=%q replyTo=%q", id, summary.ReplyTo)
	}
}

func TestViewLinksNeedNotificationIDAndFlagAndVerify(t *testing.T) {
	cfg := &config.Config{ReplyTrackingEnabled: true, ReplyMailbox: "info@cleanapp.io", ReplyTokenSecret: testReplySecret, ViewLinkBaseURL: "https://www.cleanapp.io/r/"}
	s := &EmailService{config: cfg}
	summary := models.BrandReportSummary{ReportSeqs: []int64{41, 42}}
	s.prepareViewLinks(&summary, "0123456789abcdef")
	if summary.ReportLinks != nil {
		t.Fatal("view links added with the flag off")
	}
	cfg.ViewTrackingEnabled = true
	s.prepareViewLinks(&summary, "")
	if summary.ReportLinks != nil {
		t.Fatal("view links added without a notification ID")
	}
	cfg.ViewLinkBaseURL = "http://insecure.example/r/"
	s.prepareViewLinks(&summary, "0123456789abcdef")
	if summary.ReportLinks != nil {
		t.Fatal("view links added for a non-HTTPS base URL")
	}
	cfg.ViewLinkBaseURL = "https://www.cleanapp.io/r/"
	s.prepareViewLinks(&summary, "0123456789abcdef")
	for _, seq := range []int64{41, 42} {
		token := strings.TrimPrefix(summary.ReportLinks[seq], "https://www.cleanapp.io/r/")
		id, got, ok := mailreply.ParseLinkToken(token, []byte(testReplySecret))
		if !ok || id != "0123456789abcdef" || got != seq {
			t.Fatalf("link for report %d does not verify: %q", seq, summary.ReportLinks[seq])
		}
	}
}
