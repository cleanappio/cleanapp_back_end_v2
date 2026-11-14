package reportoutcome

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func validAnalysis() Analysis {
	return Analysis{Available: true, Source: "fixture", IsValid: true, ReportKind: "firsthand", EvidenceVersion: 1, EntityRelationship: "affected", Classification: "digital", Title: "Save returns an error", Description: "The Save button returns an error after login.", BrandName: "exampleapp"}
}
func automaticInput() Input {
	return Input{Handling: Handling{Publication: "automatic", Notices: "automatic"}, ConsentVersion: 1, HasOriginal: true, HasSourceContext: true}
}

func TestReviewRespectsPrivateAuthorizationAndCoverageContext(t *testing.T) {
	now := time.Now().UTC()
	a := validAnalysis()
	old := Review(Input{Handling: Handling{Publication: "private", Notices: "none"}, HasOriginal: true, PrivacyHoldReason: "redaction_required"}, a, now)
	if old.Decision.State != Accepted || !old.Final || old.Publication.Status != "private" {
		t.Fatalf("old private report changed: %+v", old)
	}
	a.ReportKind = "coverage"
	covered := Review(automaticInput(), a, now)
	if covered.Decision.State != Accepted || covered.Publication.Status != "pending" {
		t.Fatal("legitimate sourced coverage was rejected")
	}
	i := automaticInput()
	i.HasSourceContext = false
	missing := Review(i, validAnalysis(), now)
	if missing.Decision.State != NeedsInformation || !strings.Contains(missing.Decision.Message, "new source ID") {
		t.Fatal("vague automatic report has no feasible information outcome")
	}
	i = automaticInput()
	i.Candidates = []Candidate{{Type: "report", Reference: "rpt_first", CanonicalPath: "/digital/report/rpt_first", ReasonCode: "similar_observation"}}
	cluster := Review(i, a, now)
	if cluster.Decision.State != ClusterCandidate || cluster.Relationship == nil || len(cluster.Relationship.Candidates) != 1 {
		t.Fatal("related evidence did not retain a positive cluster candidate result")
	}
}

func TestKnownPrivacyHoldDoesNotPretendPaidAnalysisRan(t *testing.T) {
	i := automaticInput()
	i.PrivacyHoldReason = "redaction_required"
	o := Review(i, Analysis{}, time.Now().UTC())
	if !o.Final || o.Processing.Status != "not_required" || o.Decision.State != Held || o.Decision.ReasonCode != "redaction_required" || !strings.Contains(o.Decision.Message, "new source ID") {
		t.Fatalf("bad privacy outcome: %+v", o)
	}
}

func TestPrivacyChecksRetainedKeysCapabilitiesAndDeclaredRestrictions(t *testing.T) {
	for _, raw := range []string{
		`{"description":"Checkout failed normally","digital_context":{"password":"fixture-only-value"}}`,
		`{"digital_context":{"tracking_token":"fixture-token"}}`,
		`{"url":"https://live.cleanapp.io/openai/#track=fixture-private-capability"}`,
		`{"url":"https://example.org/?draft_token=fixture-private-capability"}`,
		`{"description":"Contact fixture@example.invalid"}`,
		`{"description":"The upload failed","evidence_bundle":[{"type":"media","uri":"https://example.invalid/opaque"}]}`,
	} {
		if PrivacyHold([]byte(raw), 0, false, false) == "" {
			t.Fatal("sensitive public projection was accepted")
		}
	}
	if PrivacyHold([]byte(`{"description":"Save returns an error","digital_context":{"product":"Example App"}}`), 0, false, false) != "" {
		t.Fatal("clean text was held")
	}
	if PrivacyHold([]byte(`{}`), 0, false, true) != "redaction_required" {
		t.Fatal("explicit redaction was cleared")
	}
	if PrivacyHold([]byte(`{}`), 1024, false, false) != "image_privacy_review_required" {
		t.Fatal("unreviewed media was published")
	}
}

func TestPublicPrivacyIncludesTranslationsDisplayNameMediaAndEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []any
	}{
		{"translation", []any{"Title", "fixture@example.invalid", "Summary", "brand", "Brand", int64(0)}},
		{"display name", []any{"Title", "Description", "Summary", "brand", "fixture@example.invalid", int64(0)}},
		{"analysis image", []any{"Title", "Description", "Summary", "brand", "Brand", int64(100)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			rows := sqlmock.NewRows([]string{"title", "description", "summary", "brand", "display", "media"})
			rows.AddRow(tc.values[0], tc.values[1], tc.values[2], tc.values[3], tc.values[4], tc.values[5])
			mock.ExpectQuery("SELECT COALESCE\\(title").WithArgs(8).WillReturnRows(rows)
			hold, err := publicAnalysisPrivacy(context.Background(), db, 8)
			if err != nil || hold == "" {
				t.Fatalf("public projection check failed: %s %v", hold, err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT COALESCE\\(title").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"title", "description", "summary", "brand", "display", "media"}))
	mock.ExpectQuery("SELECT COALESCE\\(evidence_excerpt").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"excerpt", "impact", "reason"}).AddRow("fixture@example.invalid", "", ""))
	if hold, err := publicAnalysisPrivacy(context.Background(), db, 8); err != nil || hold == "" {
		t.Fatalf("evidence bypassed privacy check: %s %v", hold, err)
	}
}

func TestDeliveryRequiresRecordedAssessmentAndCompleteProviderProof(t *testing.T) {
	for _, tc := range []struct {
		name               string
		emailCount         int
		emailTime          any
		epcState           string
		epcTime            any
		wantEmail, wantEPC string
		wantFinal          bool
	}{
		{"routing unassessed", 0, nil, "", nil, "pending", "held", false},
		{"incomplete provider receipts", 1, nil, "sent", nil, "uncertain", "uncertain", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			o := Review(automaticInput(), validAnalysis(), time.Now().UTC())
			o.Publication.Status = "public"
			mock.ExpectQuery("SELECT COUNT").WithArgs(8, 8).WillReturnRows(sqlmock.NewRows([]string{"count", "sent_at"}).AddRow(tc.emailCount, tc.emailTime))
			if tc.emailCount == 0 {
				mock.ExpectQuery("SELECT reason,next_attempt_at").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"reason", "next"}))
				mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM sent_reports_emails").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			}
			rows := sqlmock.NewRows([]string{"status", "message", "sent", "next"})
			if tc.epcState != "" {
				rows.AddRow(tc.epcState, "123", tc.epcTime, nil)
			}
			mock.ExpectQuery("SELECT status,COALESCE").WithArgs("prod:report:8").WillReturnRows(rows)
			if tc.epcState == "" {
				mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM report_analysis").WithArgs(8).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			}
			if err := NewStore(db).delivery(context.Background(), 8, &o, false); err != nil {
				t.Fatal(err)
			}
			if o.Delivery[0].Status != tc.wantEmail || o.Delivery[1].Status != tc.wantEPC || o.Final != tc.wantFinal {
				t.Fatalf("wrong delivery outcome: %+v", o)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
