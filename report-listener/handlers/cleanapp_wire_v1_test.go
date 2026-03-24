package handlers

import (
	"testing"

	"report-listener/config"
)

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

func TestAssignCleanAppWireLaneHumanAutoPublishesEvidenceBackedReports(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{CleanAppWirePublishLaneMinTier: 2}

	lane := assignCleanAppWireLane(cfg, 2, 0.55, 1, wireLaneHumanAuto)
	if lane != wireLanePublish {
		t.Fatalf("lane = %q, want %q", lane, wireLanePublish)
	}
}

func TestAssignCleanAppWireLaneHumanAutoKeepsLowEvidenceReportsShadowed(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{CleanAppWirePublishLaneMinTier: 2}

	lane := assignCleanAppWireLane(cfg, 2, 0.9, 0, wireLaneHumanAuto)
	if lane != wireLaneShadow {
		t.Fatalf("lane = %q, want %q", lane, wireLaneShadow)
	}
}

func TestSharedURLSubmissionQualityPublishesURLOnlyShares(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{CleanAppWirePublishLaneMinTier: 2}
	payload, err := normalizeDigitalSharePayload(
		digitalShareSubmissionRequest{
			Platform:    "ios",
			CaptureMode: "share_extension",
			SourceURL:   "https://x.com/example/status/12345",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("normalizeDigitalSharePayload returned error: %v", err)
	}

	sub := buildDigitalShareWireSubmission(payload, "human")
	quality := computeCleanAppWireSubmissionQuality(sub)
	if quality < 0.50 {
		t.Fatalf("quality = %.2f, want at least 0.50 for URL-only shares", quality)
	}

	lane := assignCleanAppWireLane(cfg, 2, quality, len(sub.Report.EvidenceBundle), wireLaneHumanAuto)
	if lane != wireLanePublish {
		t.Fatalf("lane = %q, want %q", lane, wireLanePublish)
	}
}

func TestSharedImageSubmissionQualityPublishesImageOnlyShares(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{CleanAppWirePublishLaneMinTier: 2}
	payload, err := normalizeDigitalSharePayload(
		digitalShareSubmissionRequest{
			Platform:    "ios",
			CaptureMode: "share_extension",
		},
		[]digitalShareImageAttachment{{
			Bytes:    []byte("image-bytes"),
			MimeType: "image/jpeg",
			Filename: "test.jpg",
		}},
	)
	if err != nil {
		t.Fatalf("normalizeDigitalSharePayload returned error: %v", err)
	}

	sub := buildDigitalShareWireSubmission(payload, "human")
	quality := computeCleanAppWireSubmissionQuality(sub)
	if quality < 0.50 {
		t.Fatalf("quality = %.2f, want at least 0.50 for image-only shares", quality)
	}

	lane := assignCleanAppWireLane(cfg, 2, quality, len(sub.Report.EvidenceBundle), wireLaneHumanAuto)
	if lane != wireLanePublish {
		t.Fatalf("lane = %q, want %q", lane, wireLanePublish)
	}
}

func TestCleanAppWireReporterIDUsesAgentIDForHumanSubmissions(t *testing.T) {
	t.Parallel()

	auth := cleanAppWireAuthContext{
		FetcherID: "human-mobile",
		ActorKind: "human",
	}
	item := cleanAppWireIngestCoreItem{AgentID: "0xabc123"}

	got := cleanAppWireReporterID(auth, item)
	if got != "0xabc123" {
		t.Fatalf("reporter id = %q, want %q", got, "0xabc123")
	}
}

func TestCleanAppWireReporterIDFallsBackToFetcherForMachineSubmissions(t *testing.T) {
	t.Parallel()

	auth := cleanAppWireAuthContext{
		FetcherID: "fetcher-1",
		ActorKind: "machine",
	}
	item := cleanAppWireIngestCoreItem{AgentID: "agent-ignored"}

	got := cleanAppWireReporterID(auth, item)
	if got != "fetcher_v1:fetcher-1" {
		t.Fatalf("reporter id = %q, want %q", got, "fetcher_v1:fetcher-1")
	}
}
