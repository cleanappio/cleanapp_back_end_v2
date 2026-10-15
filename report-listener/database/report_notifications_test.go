package database

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNotificationSummaryCountsSendsAndProtectsRecipients(t *testing.T) {
	first := time.Date(2026, 9, 21, 18, 36, 41, 0, time.UTC)
	last := first.Add(time.Hour)
	history := buildReportNotificationHistory([]notificationReceipt{
		{Email: "support@x.ai", Source: "aggregate_brand", SentAt: &first, Legacy: true},
		{Email: "support@x.ai", Source: "aggregate_report", SentAt: &last},
		{Email: " Person@x.ai ", Source: "aggregate_report", SentAt: &last},
		{Email: "security@gmail.com", Source: "aggregate_report", SentAt: &last},
	})
	if history.EmailCount != 4 || history.RecipientCount != 3 || history.HiddenRecipientCount != 2 || history.HistoryComplete || history.ContentKind != "mixed" || history.State != "sent" {
		t.Fatalf("incorrect summary: %+v", history.ReportNotificationSummary)
	}
	if len(history.PreviewRecipients) != 1 || history.PreviewRecipients[0] != "support@x.ai" || !history.LatestSentAt.Equal(last) {
		t.Fatalf("unsafe preview/date: %+v", history.ReportNotificationSummary)
	}
	if len(history.Events) != 2 || history.Events[1].EmailCount != 3 || history.Events[0].ContentKind != "brand_digest" || history.Events[1].ContentKind != "report_email" {
		t.Fatalf("legacy digest and report emails must remain distinct: %+v", history.Events)
	}
	publicJSON, err := json.Marshal(history.ReportNotificationSummary)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"person@x.ai", "security@gmail.com", "provider", "sendgrid", "recipient_email", "\"recipients\":"} {
		if strings.Contains(string(publicJSON), private) {
			t.Fatalf("public response leaked %q: %s", private, publicJSON)
		}
	}
	fullJSON, _ := json.Marshal(history)
	if !strings.Contains(string(fullJSON), "person@x.ai") || strings.Contains(string(fullJSON), "provider") {
		t.Fatalf("subscriber log must include contacts without transport internals: %s", fullJSON)
	}
}

func TestNotificationZeroAndMissingTimesNeverInventSendEvidence(t *testing.T) {
	history := buildReportNotificationHistory(nil)
	if history.State != "none_recorded" || history.EmailCount != 0 || history.LatestSentAt != nil || len(history.Events) != 0 {
		t.Fatalf("empty history: %+v", history)
	}
	history = buildReportNotificationHistory([]notificationReceipt{{Email: "contact@x.ai", Source: "aggregate_brand", Legacy: true}})
	if history.EmailCount != 1 || history.LatestSentAt != nil || history.Events[0].SentAt != nil || history.HistoryComplete {
		t.Fatalf("missing time was invented: %+v", history)
	}
	unknown := UnavailableNotificationSummary()
	if unknown.State != "unavailable" || unknown.HistoryComplete {
		t.Fatalf("failed query must remain unknown: %+v", unknown)
	}
}

func TestPublicNotificationMailboxIsConservative(t *testing.T) {
	for _, email := range []string{"jane@x.ai", "support+person@x.ai", "Support <support@x.ai>", "support@gmail.com", "support@localhost", "not-an-address", "press@icloud.com"} {
		if publicNotificationMailbox(email) {
			t.Fatalf("unsafe public preview: %s", email)
		}
	}
	if !publicNotificationMailbox("support@x.ai") {
		t.Fatal("generic business mailbox should qualify")
	}
}

func TestReportNotificationReceiptQueryIsSequenceBoundAndStripsProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	timestamp := time.Date(2026, 9, 21, 18, 36, 41, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(reportNotificationRowsSQL)).WithArgs(1280207, 1280207).WillReturnRows(sqlmock.NewRows([]string{"recipient_email", "delivery_source", "sent_at", "is_legacy"}).AddRow("support@x.ai", "aggregate_brand", timestamp, true))
	history, err := (&Database{db: db}).ReportNotificationHistory(context.Background(), 1280207)
	if err != nil || history.EmailCount != 1 || history.HistoryComplete {
		t.Fatalf("receipt query: history=%+v err=%v", history, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationSubscriptionUsesVerifiedSubjectAndActivePaidPlan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := `(?s)SELECT EXISTS\(SELECT 1 FROM subscriptions.*customer_id = \?.*status = 'active'.*plan_type IN \('base', 'advanced', 'exclusive'\).*start_date <= CURRENT_DATE\(\)\)`
	for _, active := range []bool{false, true} {
		mock.ExpectQuery(query).WithArgs("verified-user").WillReturnRows(sqlmock.NewRows([]string{"active"}).AddRow(active))
		got, err := (&Database{db: db}).HasActiveNotificationSubscription(context.Background(), "verified-user")
		if err != nil || got != active {
			t.Fatalf("subscription = %v, %v", got, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
