package handlers

import "testing"

func TestNormalizeDigitalSharePayloadPrefersURLOverTextURL(t *testing.T) {
	payload, err := normalizeDigitalSharePayload(
		digitalShareSubmissionRequest{
			Platform:    "ios",
			CaptureMode: "share_extension",
			SharedText:  "https://twitter.com/example/status/12345",
		},
		nil,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("normalizeDigitalSharePayload returned error: %v", err)
	}

	if payload.SourceURL != "https://x.com/example/status/12345" {
		t.Fatalf("expected normalized source url, got %q", payload.SourceURL)
	}
	if payload.SharedText != "" {
		t.Fatalf("expected shared text to be cleared when it is only a URL, got %q", payload.SharedText)
	}
	if payload.SharedPayloadType != "url" {
		t.Fatalf("expected payload type url, got %q", payload.SharedPayloadType)
	}
}

func TestDigitalShareTitleAndDescription(t *testing.T) {
	title, description := digitalShareTitleAndDescription(normalizedDigitalSharePayload{
		SourceURL:   "https://example.com/post/123",
		SharedText:  "Investigate this outage ASAP\nExtra detail",
		SourceApp:   "com.example.browser",
		Platform:    "android",
		CaptureMode: "android_share_intent",
	})

	if title != "Investigate this outage ASAP" {
		t.Fatalf("unexpected title %q", title)
	}
	if description == "" {
		t.Fatal("expected description to be populated")
	}
}
