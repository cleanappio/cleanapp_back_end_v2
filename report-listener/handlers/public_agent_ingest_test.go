package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"report-listener/config"
	"report-listener/publicdiscovery"

	"github.com/gin-gonic/gin"
)

func TestPublicAgentDescriptionDoesNotInventContextOrTrust(t *testing.T) {
	req := publicAgentReportRequest{Description: "The checkout button never responds.", SourceID: "f08f6b91-582e-47bb-bb6d-7b8155db9fdd"}
	if err := validatePublicAgentRequest(&req); err != nil {
		t.Fatal(err)
	}
	sub := buildPublicAgentWireSubmission(req)
	if sub.Report.Domain != "unknown" || sub.Report.Location != nil || sub.Report.DigitalContext != nil {
		t.Fatal("invented report context")
	}
	if sub.Agent.AgentID != publicAgentActor || sub.Agent.AuthMethod != "anonymous_public" || sub.Delivery.RequestedLane != "" {
		t.Fatal("adapter assigned caller trust/publication")
	}
	if errs := validateCleanAppWireSubmissionWithContext(sub, false, true); len(errs) != 0 {
		t.Fatal(errs)
	}
	if errs := validateCleanAppWireSubmission(sub, false); len(errs) == 0 {
		t.Fatal("ordinary machines gained missing-context exception")
	}
	// Transport timestamps change, material identity does not. IP is never input.
	a, _ := cleanAppWireMaterialHash(sub)
	retry := buildPublicAgentWireSubmission(req)
	retry.SubmittedAt = time.Now().Add(time.Hour).Format(time.RFC3339)
	b, _ := cleanAppWireMaterialHash(retry)
	if a != b || sub.SourceID != retry.SourceID {
		t.Fatal("retry changed intent")
	}
	req.Description = "The button now shows a different failure."
	changed, _ := cleanAppWireMaterialHash(buildPublicAgentWireSubmission(req))
	if changed == a {
		t.Fatal("changed report did not change material identity")
	}
}

func TestPublicAgentAcceptsOnlyPrivateUUIDIntentAndFactualContext(t *testing.T) {
	lat, lng := 47.37, 8.55
	for name, req := range map[string]publicAgentReportRequest{
		"guessable key":       {Description: "observed problem", SourceID: "test123"},
		"URL as key":          {Description: "observed problem", SourceID: "https://example.com/issue"},
		"uuid v1":             {Description: "observed problem", SourceID: "f08f6b91-582e-17bb-bb6d-7b8155db9fdd"},
		"missing description": {}, "oversized": {Description: strings.Repeat("x", 8193)},
		"coordinate pair": {Description: "observed problem", Latitude: &lat},
		"invalid url":     {Description: "observed problem", URL: "file:///etc/passwd"},
		"URL credentials": {Description: "observed problem", URL: "https://user:password@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			if validatePublicAgentRequest(&req) == nil {
				t.Fatal("invalid public input accepted")
			}
		})
	}
	for _, req := range []publicAgentReportRequest{
		{Description: "observed problem"},
		{Description: "observed problem", Latitude: &lat, Longitude: &lng},
		{Description: "observed problem", URL: "https://example.com/issue", SourceID: "F08F6B91-582E-47BB-BB6D-7B8155DB9FDD"},
	} {
		if err := validatePublicAgentRequest(&req); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicAgentCapabilityIsPurposeBoundAndExpiryDoesNotRenew(t *testing.T) {
	codec, err := publicdiscovery.NewCodec("synthetic-public-agent-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{discoveryCodec: codec}
	original := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	receipt := cleanAppWireReceiptResponse{ReceiptID: "rcpt_private", ReceivedAt: original.Format(time.RFC3339)}
	first, err := h.publicAgentReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.IdempotencyReplay = true
	replay, err := h.publicAgentReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if first.TrackingExpiresAt != replay.TrackingExpiresAt || first.TrackingExpiresAt != original.Add(publicAgentReceiptTTL).Format(time.RFC3339) {
		t.Fatal("replay renewed receipt capability")
	}
	payload, err := codec.Open(first.TrackingToken)
	if err != nil || payload.Kind != publicdiscovery.KindReceipt || payload.ReceiptID != receipt.ReceiptID {
		t.Fatal("receipt capability not purpose bound")
	}
	if first.PublicationStatus != "private" || first.Status != "quarantined" || first.StatusURL != publicAgentPath+"/receipts/rcpt_private" {
		t.Fatal("public receipt promise drifted")
	}
	encoded, _ := json.Marshal(first)
	for _, field := range []string{"report_id", "public_id", "description", "canonical_path"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("receipt disclosed %s", field)
		}
	}
	// Invalid/wrong-purpose capabilities must fail before any database access.
	wrongKind, _ := codec.Seal(publicdiscovery.Payload{Kind: publicdiscovery.KindReport, ReceiptID: receipt.ReceiptID, ExpiresAtUnix: time.Now().Add(time.Hour).Unix()})
	wrongReceipt, _ := codec.Seal(publicdiscovery.Payload{Kind: publicdiscovery.KindReceipt, ReceiptID: "other", ExpiresAtUnix: time.Now().Add(time.Hour).Unix()})
	expired, _ := codec.Seal(publicdiscovery.Payload{Kind: publicdiscovery.KindReceipt, ReceiptID: receipt.ReceiptID, ExpiresAtUnix: time.Now().Add(-time.Hour).Unix()})
	for _, token := range []string{"", first.TrackingToken + "tamper", wrongKind, wrongReceipt, expired} {
		r := gin.New()
		r.GET(publicAgentPath+"/receipts/:receipt_id", h.GetPublicAgentReceiptV1)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, first.StatusURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid capability accepted: %d", w.Code)
		}
	}
}

func TestPublicAgentRejectsPrivilegeOverridesOversizeAndDisabledBeforeDatabase(t *testing.T) {
	h := &Handlers{cfg: &config.Config{PublicAgentIngestEnabled: true, CleanAppWireEnabled: true}}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"description":"problem","tier":3}`, 400},
		{`{"description":"problem","delivery":{"requested_lane":"publish"}}`, 400},
		{`{"description":"problem"} {}`, 400},
		{`{"description":"` + strings.Repeat("x", 20*1024) + `"}`, 413},
		{`{"description":"problem"}` + strings.Repeat(" ", 20*1024), 413},
	} {
		r := gin.New()
		r.POST(publicAgentPath, h.SubmitPublicAgentReportV1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, publicAgentPath, strings.NewReader(test.body)))
		if w.Code != test.status {
			t.Fatalf("got %d want %d: %s", w.Code, test.status, w.Body.String())
		}
	}
	h.cfg.PublicAgentIngestEnabled = false
	r := gin.New()
	r.POST(publicAgentPath, h.SubmitPublicAgentReportV1)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, publicAgentPath, strings.NewReader(`{"description":"problem"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled intake returned %d", w.Code)
	}
}
