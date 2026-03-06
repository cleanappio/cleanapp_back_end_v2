package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeReportAnalysedDefaultsVersion(t *testing.T) {
	raw := map[string]any{
		"report":   map[string]any{"seq": 123, "timestamp": time.Now().UTC().Format(time.RFC3339)},
		"analysis": []map[string]any{{"seq": 123, "language": "en"}},
	}
	body, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	event, err := DecodeReportAnalysed(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if event.Version != ReportAnalysedVersion {
		t.Fatalf("expected default version %q, got %q", ReportAnalysedVersion, event.Version)
	}
}
