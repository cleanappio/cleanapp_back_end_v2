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

func TestNormalizeDigitalSharePayloadAllowsMultipleImages(t *testing.T) {
	images := []digitalShareImageAttachment{
		{Bytes: []byte("image-one"), MimeType: "image/jpeg", Filename: "one.jpg"},
		{Bytes: []byte("image-two"), MimeType: "image/png", Filename: "two.png"},
	}
	payload, err := normalizeDigitalSharePayload(
		digitalShareSubmissionRequest{
			Platform:    "android",
			CaptureMode: "android_share_intent",
			SourceURL:   "https://example.com/post/1",
		},
		images,
	)
	if err != nil {
		t.Fatalf("normalizeDigitalSharePayload returned error: %v", err)
	}
	if len(payload.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(payload.Images))
	}
	if payload.SharedPayloadType != "url+image" {
		t.Fatalf("expected payload type url+image, got %q", payload.SharedPayloadType)
	}
	if payload.Images[0].SHA256Hex == "" || payload.Images[1].SHA256Hex == "" {
		t.Fatal("expected attachment hashes to be populated")
	}
}

func TestNormalizeDigitalSharePayloadInfersSourceAppFromSourceURL(t *testing.T) {
	payload, err := normalizeDigitalSharePayload(
		digitalShareSubmissionRequest{
			Platform:    "ios",
			CaptureMode: "share_extension",
			SourceURL:   "https://www.twitter.com/example/status/12345",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("normalizeDigitalSharePayload returned error: %v", err)
	}
	if payload.SourceApp != "x.com" {
		t.Fatalf("expected inferred source app x.com, got %q", payload.SourceApp)
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
