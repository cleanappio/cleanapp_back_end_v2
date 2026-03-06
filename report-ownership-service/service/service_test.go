package service

import (
	"cleanapp-common/events"
	"testing"
	"time"

	"report-ownership-service/rabbitmq"
)

func TestDecodeReportMessageAcceptsCanonicalAnalysedEvent(t *testing.T) {
	svc := &Service{}

	ts := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	msg := &rabbitmq.Message{
		Body: []byte(`{
			"version":"` + events.ReportAnalysedVersion + `",
			"report":{
				"seq":123,
				"timestamp":"2026-02-15T10:00:00Z",
				"id":"abc",
				"latitude":47.3,
				"longitude":8.5,
				"description":"raw report"
			},
			"analysis":[
				{
					"seq":123,
					"source":"gemini",
					"title":"Ownership issue",
					"brand_name":"replit",
					"brand_display_name":"Replit",
					"classification":"digital",
					"is_valid":true,
					"language":"en",
					"severity_level":0.9,
					"created_at":"2026-02-15T10:00:00Z",
					"updated_at":"2026-02-15T10:00:00Z"
				}
			]
		}`),
	}

	got, err := svc.decodeReportMessage(msg)
	if err != nil {
		t.Fatalf("decodeReportMessage returned error: %v", err)
	}
	if got.Report.Seq != 123 {
		t.Fatalf("expected report seq 123, got %d", got.Report.Seq)
	}
	if got.Report.Timestamp != ts {
		t.Fatalf("expected timestamp %v, got %v", ts, got.Report.Timestamp)
	}
	if len(got.Analysis) != 1 {
		t.Fatalf("expected 1 analysis row, got %d", len(got.Analysis))
	}
	if got.Analysis[0].BrandName != "replit" {
		t.Fatalf("expected brand_name replit, got %q", got.Analysis[0].BrandName)
	}
	if got.Analysis[0].SeverityLevel != 0.9 {
		t.Fatalf("expected severity 0.9, got %v", got.Analysis[0].SeverityLevel)
	}
}
