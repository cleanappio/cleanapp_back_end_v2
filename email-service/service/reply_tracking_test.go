package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	s.viewLinks.resolved = true // the check itself is tested below
	s.prepareViewLinks(&summary, "0123456789abcdef")
	for _, seq := range []int64{41, 42} {
		token := strings.TrimPrefix(summary.ReportLinks[seq], "https://www.cleanapp.io/r/")
		id, got, ok := mailreply.ParseLinkToken(token, []byte(testReplySecret))
		if !ok || id != "0123456789abcdef" || got != seq {
			t.Fatalf("link for report %d does not verify: %q", seq, summary.ReportLinks[seq])
		}
	}
}

// Tracked links stay off until report-listener is seen to resolve one.
func TestViewLinksStayPlainUntilReportListenerResolvesOne(t *testing.T) {
	var requests []string
	resolve := false
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		token := strings.TrimPrefix(r.URL.Path, "/api/v3/r/")
		if _, _, ok := mailreply.ParseLinkToken(token, []byte(testReplySecret)); ok && resolve {
			http.Redirect(w, r, "https://www.cleanapp.io/digital/report/rpt_public", http.StatusFound)
			return
		}
		http.Redirect(w, r, "https://www.cleanapp.io/", http.StatusFound)
	}))
	defer listener.Close()
	db, mock, _ := sqlmock.New()
	defer db.Close()
	cfg := &config.Config{ReplyTrackingEnabled: true, ViewTrackingEnabled: true, ReplyMailbox: "info@cleanapp.io",
		ReplyTokenSecret: testReplySecret, ViewLinkBaseURL: "https://www.cleanapp.io/r/", ReportListenerURL: listener.URL}
	s := &EmailService{db: db, config: cfg}
	earlier := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"notification_id", "seq"}).AddRow("aaaaaaaaaaaaaaaa", int64(7)).AddRow("bbbbbbbbbbbbbbbb", int64(8))
	}
	plain := func(when string) {
		t.Helper()
		summary := models.BrandReportSummary{ReportSeqs: []int64{41}}
		s.prepareViewLinks(&summary, "0123456789abcdef")
		if summary.ReportLinks != nil {
			t.Fatalf("%s: emails must keep plain links", when)
		}
	}

	// No earlier notification to test with.
	mock.ExpectQuery(`FROM notification_messages`).WillReturnRows(sqlmock.NewRows([]string{"notification_id", "seq"}))
	plain("no earlier notification")
	if len(requests) != 0 {
		t.Fatalf("nothing to ask report-listener about: %v", requests)
	}
	// Asked again at once: no second database query or request.
	plain("checked a moment ago")

	// report-listener sends a correctly signed link to the home page: its
	// secret differs, or the reports are no longer public.
	s.viewLinks.lastTried = time.Now().Add(-2 * viewLinkCheckInterval)
	mock.ExpectQuery(`FROM notification_messages`).WillReturnRows(earlier())
	plain("report-listener does not resolve the link")
	if len(requests) != 2 {
		t.Fatalf("each earlier report is tried: %v", requests)
	}

	// It resolves: tracked links turn on and stay on with no further checks.
	resolve = true
	s.viewLinks.lastTried = time.Now().Add(-2 * viewLinkCheckInterval)
	mock.ExpectQuery(`FROM notification_messages`).WillReturnRows(earlier())
	for i := 0; i < 2; i++ {
		summary := models.BrandReportSummary{ReportSeqs: []int64{41}}
		s.prepareViewLinks(&summary, "0123456789abcdef")
		if !strings.HasPrefix(summary.ReportLinks[41], "https://www.cleanapp.io/r/") {
			t.Fatalf("tracked links must be on once verified: %v", summary.ReportLinks)
		}
	}
	if len(requests) != 3 {
		t.Fatalf("verified once, then not asked again: %v", requests)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
