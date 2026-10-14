package evidencebackfill

import (
	"bytes"
	"cleanapp-common/evidence"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

type fakeStore struct{ saved int }

func (s *fakeStore) Candidates(context.Context, Options) ([]Candidate, error) {
	return []Candidate{{Seq: 1, Brand: "grok"}, {Seq: 2, Brand: "grok"}}, nil
}
func (s *fakeStore) Original(_ context.Context, seq int) (evidence.Original, error) {
	if seq == 2 {
		return evidence.Original{Reason: "Missing original"}, nil
	}
	return evidence.Original{Text: "I cannot submit the form."}, nil
}
func (s *fakeStore) Save(context.Context, int, evidence.Fields) error { s.saved++; return nil }

type fakeClassifier struct{ calls int }

func (c *fakeClassifier) AnalyzeEvidence(original, entity string) (evidence.Fields, error) {
	c.calls++
	return evidence.Fields{ReportKind: "firsthand", EvidenceExcerpt: "I cannot submit the form.", EntityRelationship: "affected"}, nil
}

func TestDryRunNeverWritesAndCountsUnknown(t *testing.T) {
	for _, apply := range []bool{false, true} {
		store := &fakeStore{}
		classifier := &fakeClassifier{}
		var out bytes.Buffer
		result, err := Run(context.Background(), Options{Brands: []string{"grok"}, Limit: 50, Apply: apply}, store, classifier, &out)
		if err != nil {
			t.Fatal(err)
		}
		wantSaved := 0
		if apply {
			wantSaved = 2
		}
		if store.saved != wantSaved || classifier.calls != 1 || result.Examined != 2 || result.LabelCounts["firsthand"] != 1 || result.LabelCounts["unknown"] != 1 || result.Written != wantSaved {
			t.Fatalf("apply=%v: saved=%d calls=%d result=%#v", apply, store.saved, classifier.calls, result)
		}
	}
}
func TestValidateRejectsUnboundedAndUnscopedRuns(t *testing.T) {
	for _, opts := range []Options{{Limit: 50}, {Brands: []string{"grok"}, Limit: 0}, {Brands: []string{"grok"}, Limit: 10001}} {
		if opts.Validate() == nil {
			t.Fatalf("accepted %#v", opts)
		}
	}
}

type reviewStore struct {
	originals map[int]evidence.Original
	saved     int
}

func (s *reviewStore) Candidates(context.Context, Options) ([]Candidate, error) {
	return []Candidate{{Seq: 1, Brand: "grok", Title: "Form submission fails"}, {Seq: 2, Brand: "grok", Title: "Imported report without retained original"}, {Seq: 3, Brand: "grok", Title: "Original photo report"}, {Seq: 4, Brand: "grok", Title: "Placeholder image report"}, {Seq: 5, Brand: "grok", Title: "private-contact@example.com"}}, nil
}
func (s *reviewStore) Original(_ context.Context, seq int) (evidence.Original, error) {
	return s.originals[seq], nil
}
func (s *reviewStore) Save(context.Context, int, evidence.Fields) error { s.saved++; return nil }

type reviewClassifier struct{}

func (reviewClassifier) AnalyzeEvidence(original, entity string) (evidence.Fields, error) {
	return evidence.Fields{ReportKind: "firsthand", EvidenceExcerpt: "I cannot submit the form.", EntityRelationship: "affected"}, nil
}
func (reviewClassifier) AnalyzeEvidenceImage(image []byte, original, entity string) (evidence.Fields, error) {
	return evidence.Fields{ReportKind: "firsthand", ClassificationReason: "Original photo documents the issue.", EntityRelationship: "affected"}, nil
}

func TestDryRunAuditsOriginalAvailabilityWithoutDumpingSourceData(t *testing.T) {
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	store := &reviewStore{originals: map[int]evidence.Original{
		1: {Text: "I cannot submit the form. Additional raw source detail private-contact@example.com", SourceType: "github_issue", SourceID: "private-source-identifier"},
		2: {SourceType: "twitter", Reason: "Original unavailable"},
		3: {Image: raw.Bytes(), SourceType: "human_submission"},
		4: {Image: []byte("placeholder"), SourceType: "human_submission", Reason: "Original unavailable"},
		5: {Text: "I cannot submit the form. private-contact@example.com", SourceType: "private-contact@example.com", SourceID: "private-source-identifier", RequiresRedaction: true},
	}}
	var out bytes.Buffer
	result, err := Run(context.Background(), Options{Brands: []string{"grok"}, Limit: 50}, store, reviewClassifier{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if store.saved != 0 || result.Examined != 5 {
		t.Fatalf("dry-run changed data: %#v saved=%d", result, store.saved)
	}
	decoder := json.NewDecoder(&out)
	for _, want := range []struct {
		seq           int
		title, source string
		text, media   bool
	}{
		{1, "Form submission fails", "github_issue", true, false},
		{2, "Imported report without retained original", "twitter", false, false},
		{3, "Original photo report", "human_submission", false, true},
		{4, "Placeholder image report", "human_submission", false, false},
		{5, "[Title withheld: source requires redaction]", "unknown", true, false},
	} {
		var item map[string]any
		if err := decoder.Decode(&item); err != nil {
			t.Fatal(err)
		}
		if item["seq"] != float64(want.seq) || item["title"] != want.title || item["source_type"] != want.source || item["original_text_available"] != want.text || item["original_media_available"] != want.media {
			t.Fatalf("got %#v want %#v", item, want)
		}
		encoded, _ := json.Marshal(item)
		for _, private := range []string{"private-contact@example.com", "private-source-identifier", "Additional raw source detail", base64.StdEncoding.EncodeToString(raw.Bytes())} {
			if strings.Contains(string(encoded), private) {
				t.Fatalf("raw source leaked into review output: %s", private)
			}
		}
	}
}
