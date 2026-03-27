package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync"
	"testing"
)

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

func TestExtractDigitalShareImageCandidates(t *testing.T) {
	pageURL, err := neturl.Parse("https://example.com/posts/123")
	if err != nil {
		t.Fatalf("parse page url: %v", err)
	}

	rawHTML := `
		<html>
			<head>
				<meta property="og:image" content="/images/og.png" />
				<meta name="twitter:image" content="https://cdn.example.com/twitter-card.png" />
				<link rel="image_src" href="https://cdn.example.com/legacy.png" />
				<script type="application/ld+json">
					{"@context":"https://schema.org","image":{"url":"/images/jsonld.png"}}
				</script>
			</head>
		</html>
	`

	got := extractDigitalShareImageCandidates(pageURL, rawHTML)
	want := []string{
		"https://example.com/images/og.png",
		"https://cdn.example.com/twitter-card.png",
		"https://cdn.example.com/legacy.png",
		"https://example.com/images/jsonld.png",
	}

	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d (%v)", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("candidate[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}

func TestFetchDigitalShareRemoteImagesFallsBackToCrawlerUserAgent(t *testing.T) {
	imageBytes := placeholderPNGBytes()
	var pageUserAgentsMu sync.Mutex
	var pageUserAgents []string

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/post":
			pageUserAgentsMu.Lock()
			pageUserAgents = append(pageUserAgents, r.UserAgent())
			pageUserAgentsMu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if strings.Contains(r.UserAgent(), "Slackbot-LinkExpanding") {
				_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="` + server.URL + `/image.png"></head></html>`))
				return
			}
			_, _ = w.Write([]byte(`<html><head><title>No preview yet</title></head></html>`))
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	attachments, err := fetchDigitalShareRemoteImages(context.Background(), server.Client(), server.URL+"/post", digitalShareRemoteFetchOptions{
		MaxImages:     2,
		MaxTotalBytes: maxDigitalShareTotalBytes,
	})
	if err != nil {
		t.Fatalf("fetchDigitalShareRemoteImages returned error: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachment count = %d, want 1", len(attachments))
	}
	pageUserAgentsMu.Lock()
	gotUserAgents := append([]string(nil), pageUserAgents...)
	pageUserAgentsMu.Unlock()
	if !strings.Contains(strings.Join(gotUserAgents, "\n"), digitalShareSlackbotUserAgent) {
		t.Fatalf("expected crawler fallback user-agent, got %v", gotUserAgents)
	}
	if attachments[0].MimeType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", attachments[0].MimeType)
	}
	if len(attachments[0].Bytes) != len(imageBytes) {
		t.Fatalf("image bytes len = %d, want %d", len(attachments[0].Bytes), len(imageBytes))
	}
}

func TestEnrichDigitalSharePayloadWithRemoteImagesUpdatesPayloadType(t *testing.T) {
	imageBytes := placeholderPNGBytes()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/post":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="` + server.URL + `/image.png"></head></html>`))
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	h := &Handlers{}
	payload := normalizedDigitalSharePayload{
		SourceURL:         server.URL + "/post",
		SharedPayloadType: "url",
	}

	got := h.enrichDigitalSharePayloadWithRemoteImages(context.Background(), payload)
	if len(got.Images) != 1 {
		t.Fatalf("image count = %d, want 1", len(got.Images))
	}
	if got.RemoteImageCount != 1 {
		t.Fatalf("remote image count = %d, want 1", got.RemoteImageCount)
	}
	if got.SharedPayloadType != "url+image" {
		t.Fatalf("payload type = %q, want url+image", got.SharedPayloadType)
	}
}
