package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"cleanapp-common/reportoutcome"
	"github.com/gin-gonic/gin"
	"report-listener/config"
)

// Expected hash was generated with the unmodified submission type and hash
// function from main 652e135d3ca80ee94addfb0c436e7b8571a8256b.
const legacyOutcomeWireFixture = `{"schema_version":"cleanapp-wire.v1","source_id":"openai_00000000-0000-4000-8000-000000000001","submitted_at":"2026-11-14T08:00:00Z","agent":{"agent_id":"fixture-agent","agent_name":"CleanApp for ChatGPT","agent_type":"assistant","operator_type":"internal","auth_method":"api_key","software_version":"1.0.4","execution_mode":"human_confirmed"},"provenance":{"generation_method":"human_assisted","human_in_loop":true},"report":{"domain":"digital","problem_type":"software_bug","title":"Save fails","description":"I cannot save on Example App.","language":"en","confidence":0.5,"tags":["chatgpt","human_reviewed"],"privacy":{"requires_redaction":true},"digital_context":{"product_or_site":"Example App"}},"delivery":{"requested_lane":"quarantine"}}`

func TestLegacyWireOutcomeHashGoldenAndNewConsentBinding(t *testing.T) {
	var sub cleanAppWireSubmission
	if err := json.Unmarshal([]byte(legacyOutcomeWireFixture), &sub); err != nil {
		t.Fatal(err)
	}
	hash, err := cleanAppWireMaterialHash(sub)
	if err != nil || hash != "f606013be4824d3e6d16466c8de052b8e10e25a10dc298cedbadbc0d15d6343a" {
		t.Fatalf("legacy retry hash changed: %s %v", hash, err)
	}
	if sub.Delivery.Handling != nil {
		t.Fatal("old draft acquired new authorization")
	}
	sub.Delivery.Handling = &reportoutcome.Handling{Publication: "automatic", Notices: "automatic"}
	changed, err := cleanAppWireMaterialHash(sub)
	if err != nil || changed == hash {
		t.Fatal("new authorization was not bound to immutable report material")
	}
}

func TestPublicHandlingOmissionAndExplicitPermissions(t *testing.T) {
	request := publicAgentReportRequest{Description: "The Save button returns an error on Example App.", SourceID: "00000000-0000-4000-8000-000000000001"}
	if err := validatePublicAgentRequest(&request); err != nil {
		t.Fatal(err)
	}
	legacy := buildPublicAgentWireSubmission(request)
	if request.Handling != nil || legacy.Delivery.Handling != nil {
		t.Fatal("old public request acquired automatic permission")
	}
	oldHash, _ := cleanAppWireMaterialHash(legacy)
	request.Handling = &reportoutcome.Handling{Publication: "automatic", Notices: "none"}
	if err := validatePublicAgentRequest(&request); err != nil {
		t.Fatal(err)
	}
	fresh := buildPublicAgentWireSubmission(request)
	freshHash, _ := cleanAppWireMaterialHash(fresh)
	if fresh.Delivery.Handling == nil || fresh.Delivery.Handling.Notices != "none" || freshHash == oldHash {
		t.Fatal("explicit handling was dropped or unbound")
	}
	request.Handling = &reportoutcome.Handling{Publication: "publish_everything"}
	if err := validatePublicAgentRequest(&request); err == nil {
		t.Fatal("unknown permission mode was accepted")
	}
}

func TestWireOutcomeHandlersNeverCachePrivateResponses(t *testing.T) {
	h := &Handlers{cfg: &config.Config{}}
	for _, handler := range []gin.HandlerFunc{h.SubmitCleanAppWireV1, h.BatchSubmitCleanAppWireV1, h.GetCleanAppWireReceiptV1, h.GetCleanAppWireStatusV1} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("POST", "/", nil)
		handler(ctx)
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private receipt response was cacheable")
		}
	}
}
