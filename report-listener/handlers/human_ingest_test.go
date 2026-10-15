package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"report-listener/config"
)

func webImage(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var err error
	if format == "png" {
		err = png.Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestHumanWebSubmissionAcceptsTextOrActualBoundedImage(t *testing.T) {
	for _, req := range []humanReportSubmissionRequest{
		{Annotation: "I cannot finish checkout; the pay button does nothing."},
		{Image: webImage(t, "png", 64, 64)},
		{Annotation: "Blocked access ramp", Image: webImage(t, "jpeg", 64, 64)},
	} {
		if err := validateHumanWebSubmission(req); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHumanWebSubmissionRejectsEmptyOversizedAndMalformedEvidence(t *testing.T) {
	pngImage := webImage(t, "png", 64, 64)
	for name, req := range map[string]humanReportSubmissionRequest{
		"empty": {}, "whitespace": {Annotation: " \n\t"},
		"text byte limit":             {Annotation: strings.Repeat("é", 4097)},
		"invalid text":                {Annotation: string([]byte{0xff})},
		"image byte limit":            {Image: make([]byte, maxHumanWebImageBytes+1)},
		"arbitrary bytes":             {Image: []byte("not an image")},
		"svg":                         {Image: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		"valid header truncated body": {Image: pngImage[:33]},
		"too wide":                    {Image: webImage(t, "png", 4097, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if validateHumanWebSubmission(req) == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}

func TestHumanWebDoesNotInventLocationDomainOrAuthenticatedIdentity(t *testing.T) {
	req := humanReportSubmissionRequest{Channel: "web", Annotation: "I saw a broken door", ReporterID: "claimed-wallet", DeviceID: "claimed-device", SessionID: "self-asserted-session"}
	sub := buildHumanWireSubmission(req, "human")
	if sub.Report.Location != nil || sub.Report.DigitalContext != nil || sub.Report.Domain != "unknown" {
		t.Fatalf("invented context: %+v", sub.Report)
	}
	if sub.Agent.AuthMethod != "anonymous_client" || sub.Agent.AgentID != "human-web" {
		t.Fatal("self-asserted identity trusted")
	}
	if sub.Delivery.RequestedLane != wireLaneShadow {
		t.Fatal("anonymous web bypassed analysis gate")
	}
	if sub.Extensions["web_intake_pending_analysis"] != true {
		t.Fatal("missing deterministic downstream policy marker")
	}
	if errs := validateCleanAppWireSubmissionWithContext(sub, false, true); len(errs) != 0 {
		t.Fatalf("canonical contextless report rejected: %+v", errs)
	}
	if errs := validateCleanAppWireSubmission(sub, false); len(errs) == 0 {
		t.Fatal("machine validator accepted missing context")
	}
}

func TestMobileLocationAndPublishLaneRemainCompatible(t *testing.T) {
	lat, lng := 47.37, 8.55
	sub := buildHumanWireSubmission(humanReportSubmissionRequest{Channel: "mobile", Latitude: &lat, Longitude: &lng, Image: webImage(t, "png", 32, 32)}, "human")
	if sub.Report.Domain != "physical" || sub.Report.Location == nil || sub.Report.Location.Lat != lat || sub.Report.Location.Lng != lng || sub.Delivery.RequestedLane != wireLaneHumanAuto {
		t.Fatal("mobile behavior changed")
	}
}

func TestHumanWebRetriesPreserveMaterialHashWithoutObservationTime(t *testing.T) {
	req := humanReportSubmissionRequest{Channel: "web", SourceID: "web_a-random-id", SessionID: "same-session", Annotation: "I got an error"}
	a := buildHumanWireSubmission(req, "human")
	b := buildHumanWireSubmission(req, "human")
	ha, _ := cleanAppWireMaterialHash(a)
	hb, _ := cleanAppWireMaterialHash(b)
	if ha != hb || a.ObservedAt != "" || a.Report.Location != nil {
		t.Fatal("retry identity or unknown context changed")
	}
	req.Annotation = "I got a different error"
	c := buildHumanWireSubmission(req, "human")
	hc, _ := cleanAppWireMaterialHash(c)
	if hc == ha {
		t.Fatal("changed payload did not conflict")
	}
}

func TestHumanWebReceiptOnlyLinksAnalyzedPublicClassification(t *testing.T) {
	for _, tc := range []struct {
		visibility, classification string
		analyzed                   bool
		want                       string
	}{
		{"shadow", "", false, ""}, {"shadow", "digital", true, ""}, {"public", "", false, ""},
		{"public", "unknown", true, ""}, {"public", "digital", true, "/digital/report/r_safe"}, {"public", "physical", true, "/physical/report/r_safe"},
	} {
		r := humanReceiptWithProcessingState(humanReportReceiptResponse{}, "r_safe", tc.visibility, "human-web", tc.analyzed, tc.classification)
		if r.CanonicalPath != tc.want {
			t.Fatalf("%+v: %+v", tc, r)
		}
		if tc.visibility == "shadow" && tc.analyzed && r.PublicationStatus != "review_required" {
			t.Fatal("hidden review state")
		}
	}
}

func TestBrowserCannotBypassWebValidationByClaimingMobileChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{cfg: &config.Config{HumanIngestEnabled: true}}
	router := gin.New()
	router.POST("/submit", h.SubmitHumanReportV1)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"channel":"mobile","image":"bm90IGFuIGltYWdl"}`))
	r.Header.Set("Origin", "https://www.cleanapp.io")
	r.RemoteAddr = "192.0.2.80:1234"
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "valid JPEG or PNG") {
		t.Fatalf("browser bypassed validation: %d %s", w.Code, w.Body.String())
	}
}

func TestHumanWebRateLimitCannotBeResetByChangingSessionOrAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{cfg: &config.Config{HumanIngestEnabled: true}}
	router := gin.New()
	for _, path := range []string{"/api/v1/submit", "/api/v3/submit", "/api/v4/submit"} {
		router.POST(path, h.SubmitHumanReportV1)
	}
	for i, path := range []string{"/api/v1/submit", "/api/v3/submit", "/api/v4/submit"} {
		body, _ := json.Marshal(map[string]string{"channel": "web", "session_id": path, "annotation": ""})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.RemoteAddr = "192.0.2.81:1234"
		router.ServeHTTP(w, r)
		want := http.StatusBadRequest
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("request %d: %d want %d", i, w.Code, want)
		}
	}
}

type webQueuePublisher struct {
	route   string
	message map[string]interface{}
	err     error
}

func (*webQueuePublisher) IsConnected() bool         { return true }
func (*webQueuePublisher) Close() error              { return nil }
func (*webQueuePublisher) Publish(interface{}) error { return nil }
func (p *webQueuePublisher) PublishWithRoutingKey(route string, message interface{}) error {
	p.route = route
	p.message = message.(map[string]interface{})
	return p.err
}

func TestWebQueueUsesExistingHumanLaneWithoutInventedCoordinates(t *testing.T) {
	p := &webQueuePublisher{}
	h := &Handlers{cfg: &config.Config{RabbitRawReportRoutingKey: "report.raw", RabbitHumanReportRoutingKey: "report.human"}, rabbitmqPublisher: p}
	auth := cleanAppWireAuthContext{ActorKind: "human", Channel: "web", FetcherID: "human-web"}
	item := cleanAppWireIngestCoreItem{SourceID: "web_retry", Description: "A real observation"}
	if code := h.queueCleanAppWireCoreItem(auth, item, 42, "shadow"); code != "" {
		t.Fatal(code)
	}
	if p.route != "report.human" || p.message["seq"] != 42 || p.message["visibility"] != "shadow" {
		t.Fatalf("wrong existing queue payload: %+v", p)
	}
	if _, ok := p.message["latitude"]; ok {
		t.Fatal("fabricated queued latitude")
	}
	if _, ok := p.message["longitude"]; ok {
		t.Fatal("fabricated queued longitude")
	}
	p.err = errors.New("broker failure")
	if code := h.queueCleanAppWireCoreItem(auth, item, 42, "shadow"); code != "QUEUE_PUBLISH_FAILED" {
		t.Fatal("failed queue publish acknowledged")
	}
}
