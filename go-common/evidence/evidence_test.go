package evidence

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDefensiveLabelsAndLiteralEvidence(t *testing.T) {
	original := "I cannot save my session. It lost two hours of work."
	tests := []struct{ name, response, kind, quote string }{
		{"firsthand", `{"report_kind":"FIRSTHAND","evidence_excerpt":"I cannot save my session.","entity_relationship":"affected"}`, "firsthand", "I cannot save my session."},
		{"legacy", `{}`, "unknown", ""},
		{"invalid enum", `{"report_kind":"verified","entity_relationship":10}`, "unknown", ""},
		{"malformed fields", `{"report_kind":true,"evidence_excerpt":[]}`, "unknown", ""},
		{"invented quote", `{"report_kind":"firsthand","evidence_excerpt":"I lost a million dollars"}`, "unknown", ""},
		{"missing quote", `{"report_kind":"firsthand"}`, "unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.response, original)
			if err != nil {
				t.Fatal(err)
			}
			if got.ReportKind != tt.kind || got.EvidenceExcerpt != tt.quote {
				t.Fatalf("got %#v", got)
			}
		})
	}
	got, err := Parse(`{"report_kind":"firsthand","evidence_excerpt":"I cannot save my session."}`, "")
	if err != nil || got.ReportKind != "unknown" || got.EvidenceExcerpt != "" {
		t.Fatalf("missing original: %#v %v", got, err)
	}
}

func TestExcerptBoundsPreserveLiteralUnicode(t *testing.T) {
	original := strings.Repeat("界", MaxExcerptRunes+10)
	got, err := Parse(`{"report_kind":"firsthand","evidence_excerpt":"`+original+`"}`, original)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got.EvidenceExcerpt)) != MaxExcerptRunes || !strings.Contains(original, got.EvidenceExcerpt) {
		t.Fatalf("bad bounded excerpt %q", got.EvidenceExcerpt)
	}
}

func TestOriginalSelectionNeverUsesImportedSummary(t *testing.T) {
	for _, source := range []string{"github_issue", "twitter", "bluesky", "unknown_import"} {
		got := SelectOriginal(source, "I broke the app (generated summary)", "", "")
		if got.Text != "" {
			t.Fatalf("%s used generated summary", source)
		}
	}
	if got := SelectOriginal("github_issue", "summary", "", "Original issue body"); got.Text != "Original issue body" {
		t.Fatalf("got %#v", got)
	}
	if got := SelectOriginal("digital_share", "Human report submission", "I hit an error", ""); got.Text != "I hit an error" {
		t.Fatalf("got %#v", got)
	}
	if got := SelectOriginal("", "Human report submission", "", ""); got.Text != "" {
		t.Fatalf("placeholder treated as original: %#v", got)
	}
	if got := SelectOriginal("", "I found a blocked ramp", "", ""); got.Text != "I found a blocked ramp" {
		t.Fatalf("direct text missing: %#v", got)
	}
}

func TestWireSourceIdentity(t *testing.T) {
	for _, tt := range []struct{ sourceID, source, id string }{
		{"github_issue:123", "github_issue", "123"},
		{"twitter:999", "twitter", "999"},
		{"bluesky:at://did:plc:abc/app.bsky.feed.post/xyz", "bluesky", "at://did:plc:abc/app.bsky.feed.post/xyz"},
		{"unrecognized:1", "unknown", ""},
		{"at://did:plc:abc/app.bsky.feed.post/xyz", "bluesky", "at://did:plc:abc/app.bsky.feed.post/xyz"},
	} {
		source, id := WireSourceIdentity(tt.sourceID)
		if source != tt.source || id != tt.id {
			t.Fatalf("%s: %s/%s", tt.sourceID, source, id)
		}
	}
}

func TestPhotoOnlyEvidenceRequiresActualOriginalMedia(t *testing.T) {
	fields := Fields{ReportKind: "firsthand", EntityRelationship: "affected", ClassificationReason: "Original photo shows a blocked ramp."}
	if got := fields.NormalizeWithMedia("", true); got.ReportKind != "firsthand" {
		t.Fatalf("photo-only original lost: %#v", got)
	}
	if got := fields.NormalizeWithMedia("", false); got.ReportKind != "unknown" {
		t.Fatalf("missing original image accepted: %#v", got)
	}
	// Attaching an image never legitimizes an invented quote.
	fields.EvidenceExcerpt = "fabricated image quote"
	if got := fields.NormalizeWithMedia("", true); got.EvidenceExcerpt != "" {
		t.Fatalf("invented quote retained: %#v", got)
	}
}

func TestRetainedOriginalDoesNotReadGeneratedWireDescriptions(t *testing.T) {
	if got := RetainedOriginal([]byte(`{"description":"I had an error","title":"A complaint"}`)); got != "" {
		t.Fatal(got)
	}
	if got := RetainedOriginal([]byte(`{"digital_context":{"metadata":{"original_text":"Real source text"}}}`)); got != "Real source text" {
		t.Fatal(got)
	}
}

type transientClassifier struct{ fail bool }

func (c transientClassifier) AnalyzeEvidence(original, entity string) (Fields, error) {
	if c.fail {
		return Fields{}, errors.New("transient provider failure")
	}
	return Fields{ReportKind: "firsthand", EvidenceExcerpt: "I hit an error", EntityRelationship: "affected"}, nil
}

func TestTransientEvidenceFailuresRemainRetryableWhileUnknownCompletes(t *testing.T) {
	original := Original{Text: "I hit an error"}
	for _, tt := range []struct {
		name          string
		source        Original
		resolutionErr error
		client        Classifier
		wantKind      string
		wantVersion   int
		wantErr       bool
	}{
		{"provider failed", original, nil, transientClassifier{fail: true}, "unknown", 0, true},
		{"original resolution failed", Original{}, errors.New("temporary database failure"), transientClassifier{}, "unknown", 0, true},
		{"successful retry", original, nil, transientClassifier{}, "firsthand", Version, false},
		{"successful insufficient evidence", Original{Reason: "Missing original"}, nil, transientClassifier{}, "unknown", Version, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fields, version, err := ForPersistence(tt.client, tt.source, "Grok", tt.resolutionErr)
			if fields.ReportKind != tt.wantKind || version != tt.wantVersion || (err != nil) != tt.wantErr {
				t.Fatalf("fields=%#v version=%d err=%v", fields, version, err)
			}
		})
	}
}
