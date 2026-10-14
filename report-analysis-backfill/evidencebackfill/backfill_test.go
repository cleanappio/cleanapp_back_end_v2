package evidencebackfill

import (
	"bytes"
	"cleanapp-common/evidence"
	"context"
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
