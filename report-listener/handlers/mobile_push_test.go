package handlers

import (
	"reflect"
	"testing"
	"time"

	"report-listener/models"
)

func TestReportDeliveryPushDataIncludesEscalationTargetAndReceipt(t *testing.T) {
	sentAt := time.Date(2026, time.October, 10, 16, 36, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	got := buildReportDeliveryPushData(123, "sent", 3, "report-public-id", []models.ReportDeliveryRecipient{{
		Email: "maintenance@example.com", DisplayName: "Maintenance", SentAt: &sentAt,
	}})
	want := map[string]string{
		"seq": "123", "status": "sent", "public_id": "report-public-id",
		"navigate_to": "my_report_details", "initial_section": "escalation_log",
		"recipient_count": "3", "recipient_email": "maintenance@example.com",
		"recipient_name": "Maintenance", "sent_at": "2026-10-10T14:36:00Z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery navigation/receipt contract changed: got %#v, want %#v", got, want)
	}
}

func TestReportDeliveryPushDataKeepsMissingReceiptTimeAbsent(t *testing.T) {
	got := buildReportDeliveryPushData(123, "sent", 1, "", []models.ReportDeliveryRecipient{{
		Email: "maintenance@example.com", Organization: "Maintenance team",
	}})
	if got["recipient_name"] != "Maintenance team" || got["recipient_count"] != "1" {
		t.Fatalf("recipient metadata not retained: %#v", got)
	}
	if _, exists := got["sent_at"]; exists {
		t.Fatal("missing delivery receipt time must not be replaced by processing/current time")
	}
	if _, exists := got["public_id"]; exists {
		t.Fatal("missing public ID must remain absent; seq still identifies the owned report")
	}
}

func TestReportProcessedPushDataOpensLogWithoutClaimingDelivery(t *testing.T) {
	got := buildReportDeliveryPushData(123, "processed_no_delivery", 0, "", nil)
	want := map[string]string{
		"seq": "123", "status": "processed_no_delivery", "recipient_count": "0",
		"navigate_to": "my_report_details", "initial_section": "escalation_log",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("processed report should expose the log without a delivery receipt: %#v", got)
	}
}

func TestBuildReportDeliveryPushMessageSingleRecipient(t *testing.T) {
	sentAt := time.Date(2026, time.March, 21, 14, 36, 0, 0, time.UTC)
	title, body := buildReportDeliveryPushMessage("sent", 1, []models.ReportDeliveryRecipient{
		{
			Email:       "schule@adliswil.ch",
			DisplayName: "Schulhaus Kopfholz",
			SentAt:      &sentAt,
		},
	})

	if title != "Report sent" {
		t.Fatalf("unexpected title: %q", title)
	}
	want := "Your report was sent to Schulhaus Kopfholz at schule@adliswil.ch on 2026-03-21 14:36 UTC."
	if body != want {
		t.Fatalf("unexpected body:\nwant: %q\ngot:  %q", want, body)
	}
}

func TestBuildReportDeliveryPushMessageMultipleRecipients(t *testing.T) {
	sentAt := time.Date(2026, time.March, 21, 14, 36, 0, 0, time.UTC)
	_, body := buildReportDeliveryPushMessage("sent", 3, []models.ReportDeliveryRecipient{
		{
			Email:        "bau.planung@adliswil.ch",
			Organization: "Bau und Planung",
			SentAt:       &sentAt,
		},
	})

	want := "Your report was sent to Bau und Planung at bau.planung@adliswil.ch on 2026-03-21 14:36 UTC and 2 more recipient(s)."
	if body != want {
		t.Fatalf("unexpected body:\nwant: %q\ngot:  %q", want, body)
	}
}
