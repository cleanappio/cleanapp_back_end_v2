package handlers

import (
	"testing"
	"time"

	"report-listener/models"
)

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
