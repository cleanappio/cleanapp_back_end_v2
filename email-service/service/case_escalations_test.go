package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"email-service/config"
	"email-service/email"

	"github.com/DATA-DOG/go-sqlmock"
)

type caseTestSender struct {
	aggregateTestSender
	customRecipients []string
	customResults    map[string][]email.CustomEmailSendResult
}

func (s *caseTestSender) SendCustomEmails(recipients []string, _, _, _ string) []email.CustomEmailSendResult {
	if len(recipients) != 1 {
		panic("case sends must retain individual recipient results")
	}
	s.customRecipients = append(s.customRecipients, recipients[0])
	return s.customResults[recipients[0]]
}

func TestCaseAcceptedResultSurvivesSecondaryHistoryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 15, 15, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	sender := &caseTestSender{customResults: map[string][]email.CustomEmailSendResult{
		"support@example.org": {{Email: "support@example.org", Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted-123", SentAt: at}},
	}}
	mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs("support@example.org").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs("support@example.org").WillReturnError(errors.New("secondary history unavailable"))
	targetID := int64(42)
	results := (&EmailService{db: db, email: sender, config: &config.Config{}}).SendCaseEscalationEmails(context.Background(), "Issue", "Original evidence", "", []CaseEscalationRecipient{{TargetID: &targetID, Email: " Support@Example.org ", DeliverySource: "case_target"}})
	if len(results) != 1 {
		t.Fatalf("wrong result count: %v", results)
	}
	r := results[0]
	if r.Status != "sent" || r.ProviderMessageID != "accepted-123" || r.SentAt == nil || !r.SentAt.Equal(at.UTC()) || r.SentAt.Location() != time.UTC || r.Error != "" || r.Email != "support@example.org" || r.TargetID == nil || *r.TargetID != targetID || r.DeliverySource != "case_target" {
		t.Fatalf("accepted send was erased or secondary error leaked into result: %#v", r)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCaseDryRunHasNoDatabaseOrSendingEffects(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sender := &caseTestSender{}
	targetID := int64(42)
	results := (&EmailService{db: db, email: sender, config: &config.Config{DryRun: true}}).SendCaseEscalationEmails(context.Background(), "Issue", "Evidence", "", []CaseEscalationRecipient{{TargetID: &targetID, Email: " Support@Example.org "}, {Email: "other@example.org", DeliverySource: "case_cc"}})
	if len(results) != 2 || len(sender.customRecipients) != 0 {
		t.Fatalf("dry run sent email or lost recipients: %v sends=%v", results, sender.customRecipients)
	}
	for _, r := range results {
		if r.Status != "dry_run" || r.ProviderMessageID != "" || r.SentAt != nil || r.Error != "" {
			t.Fatalf("dry run fabricated acceptance: %#v", r)
		}
	}
	if results[0].Email != "support@example.org" || results[0].DeliverySource != "case_target" || results[0].TargetID == nil || *results[0].TargetID != targetID || results[1].DeliverySource != "case_cc" {
		t.Fatalf("dry run lost routing metadata: %v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCaseOptOutAndPartialResultsRemainPerRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 10, 15, 13, 30, 0, 0, time.UTC)
	sender := &caseTestSender{customResults: map[string][]email.CustomEmailSendResult{
		"accepted@example.org": {{Status: "sent", Provider: "google_workspace", ProviderMessageID: "accepted", SentAt: at}},
		"failed@example.org":   {{Status: "failed", Provider: "google_workspace", Error: "transport rejected"}},
	}}
	for _, recipient := range []string{"optedout@example.org", "accepted@example.org", "failed@example.org"} {
		count := 0
		if recipient == "optedout@example.org" {
			count = 1
		}
		mock.ExpectQuery(`SELECT COUNT\(\*\).*FROM opted_out_emails`).WithArgs(recipient).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
		if recipient == "accepted@example.org" {
			mock.ExpectExec(`INSERT INTO email_recipient_history`).WithArgs(recipient).WillReturnResult(sqlmock.NewResult(1, 1))
		}
	}
	results := (&EmailService{db: db, email: sender, config: &config.Config{}}).SendCaseEscalationEmails(context.Background(), "Issue", "Evidence", "", []CaseEscalationRecipient{{Email: "invalid"}, {Email: "optedout@example.org"}, {Email: "accepted@example.org"}, {Email: "failed@example.org"}})
	statuses := []string{}
	for _, r := range results {
		statuses = append(statuses, r.Status)
	}
	if !reflect.DeepEqual(statuses, []string{"invalid_email", "opted_out", "sent", "failed"}) || !reflect.DeepEqual(sender.customRecipients, []string{"accepted@example.org", "failed@example.org"}) {
		t.Fatalf("recipient policy or partial outcomes changed: results=%v sends=%v", results, sender.customRecipients)
	}
	if results[2].ProviderMessageID != "accepted" || results[2].SentAt == nil || !results[2].SentAt.Equal(at) || results[3].Error != "transport rejected" || results[3].SentAt != nil {
		t.Fatalf("partial send evidence lost: %v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
