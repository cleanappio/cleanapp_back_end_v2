package handlers

import "testing"

func TestLegacySourceMetadataRetainsOriginalSeparateFromSummary(t *testing.T) {
	item := preparedBulkIngestItem{
		createdAt:   "2025-07-20T10:44:43Z",
		url:         "https://example.com/observations/42",
		description: "Generated summary",
		metadata:    map[string]interface{}{"original_text": "I encountered the failure.", "original_text_truncated": false},
	}
	got := legacySourceMetadata(item)
	if got["original_text"] != "I encountered the failure." || got["source_published_at"] != item.createdAt || got["source_url"] != item.url {
		t.Fatalf("source snapshot not retained: %#v", got)
	}
	if _, ok := item.metadata["source_published_at"]; ok {
		t.Fatal("source metadata helper mutated the request")
	}
}

func TestLegacyMissingOriginalDoesNotUseGeneratedDescription(t *testing.T) {
	got := legacySourceMetadata(preparedBulkIngestItem{description: "Generated summary"})
	if _, ok := got["original_text"]; ok {
		t.Fatal("generated description became original evidence")
	}
}
