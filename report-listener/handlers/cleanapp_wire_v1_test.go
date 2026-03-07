package handlers

import "testing"

func TestCleanAppWireMaterialHashIgnoresTransportFields(t *testing.T) {
	base := cleanAppWireSubmission{
		SchemaVersion: "cleanapp-wire.v1",
		SourceID:      "source-123",
		ObservedAt:    "2026-03-07T10:00:00Z",
	}
	base.Agent.AgentID = "agent-1"
	base.Agent.AgentType = "scraper"
	base.Agent.AuthMethod = "api_key_signature"
	base.Report.Domain = "digital"
	base.Report.ProblemType = "spam"
	base.Report.Title = "Repeated spam wave"
	base.Report.Description = "Detected coordinated spam."
	base.Report.Confidence = 0.82
	base.Report.Location = &struct {
		Kind            string  `json:"kind,omitempty"`
		Lat             float64 `json:"lat"`
		Lng             float64 `json:"lng"`
		Geohash         string  `json:"geohash,omitempty"`
		AddressText     string  `json:"address_text,omitempty"`
		PlaceConfidence float64 `json:"place_confidence,omitempty"`
	}{
		Lat: 47.36,
		Lng: 8.55,
	}

	first := base
	first.SubmissionID = "subm-1"
	first.SubmittedAt = "2026-03-07T10:01:00Z"
	first.Agent.Signature = "sig-a"

	second := base
	second.SubmissionID = "subm-2"
	second.SubmittedAt = "2026-03-07T10:05:00Z"
	second.Agent.Signature = "sig-b"

	h1, err := cleanAppWireMaterialHash(first)
	if err != nil {
		t.Fatalf("first hash failed: %v", err)
	}
	h2, err := cleanAppWireMaterialHash(second)
	if err != nil {
		t.Fatalf("second hash failed: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected idempotent material hash to ignore transport fields, got %q != %q", h1, h2)
	}
}
