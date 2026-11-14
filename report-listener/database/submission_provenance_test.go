package database

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"report-listener/models"
)

func retainedSubmission(t *testing.T, value string) *publicSubmissionProvenance {
	t.Helper()
	var p publicSubmissionProvenance
	if err := json.Unmarshal([]byte(value), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestHistoricalAgentProvenancePreservesKnownContextWithoutInventingClientOrDates(t *testing.T) {
	received := time.Date(2026, 11, 14, 20, 6, 21, 0, time.UTC)
	r := evidenceRecord{Seq: 1, ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", SourcePlatform: "web", IngestedAt: received,
		SubmissionProvenance: retainedSubmission(t, `{"product":"x.com / Cloudflare","description":"The affected page is https://x.com/account/access. Retrying https://x.com/account/access did not help."}`)}
	ev := r.publicEvidence()
	if ev.IntakeMethod != "agent_submission" || ev.IntakeLabel != "Agent-assisted submission" || stringValue(ev.IntakeChannel) != "agent_api" {
		t.Fatalf("wrong acquisition: %+v", ev)
	}
	if stringValue(ev.SubmissionTransport) != "agent_api" || ev.SubmissionClient != nil || ev.SubmissionClientClaimed || ev.SourcePlatform != nil {
		t.Fatalf("fabricated source/client: %+v", ev)
	}
	if ev.ObservedAt != nil || ev.SourcePublishedAt != nil || ev.ObservedAtBasis != "not_recorded" || ev.SourcePublishedAtBasis != "not_recorded" || ev.IngestedAt == nil || !ev.IngestedAt.Equal(received) {
		t.Fatalf("dates conflated: %+v", ev)
	}
	if stringValue(ev.ReportedProductOrSite) != "x.com / Cloudflare" || len(ev.ReferencedURLs) != 1 || ev.ReferencedURLs[0] != "https://x.com/account/access" || ev.SourceReferenceCount == nil || *ev.SourceReferenceCount != 0 {
		t.Fatalf("affected URL became source evidence: %+v", ev)
	}
	if ev.ReportKind != "unknown" || ev.ValidationStatus != "not_independently_reproduced" || ev.UpstreamStatus != "unknown" || ev.ResolutionStatus != "unknown" {
		t.Fatalf("unsupported status claim: %+v", ev)
	}
}

func TestProvidedSourceContextIsClaimedAndDistinctReferencesAreNotUsers(t *testing.T) {
	observed := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", ObservedAt: &observed,
		SubmissionProvenance: retainedSubmission(t, `{"client":{"transport":"mcp","name":"grok","name_source":"caller_provided"},"reporter_context":"firsthand","source_url":"https://x.com/person/status/1234567890123456789?utm_source=x","upstream":[{"kind":"url","value":"https://twitter.com/person/status/1234567890123456789"}],"published_at":"2026-11-11T10:00:00+02:00"}`)}
	ev := r.publicEvidence()
	if stringValue(ev.SubmissionTransport) != "mcp" || stringValue(ev.SubmissionClient) != "grok" || !ev.SubmissionClientClaimed || stringValue(ev.ReporterContext) != "firsthand" {
		t.Fatalf("claim lost: %+v", ev)
	}
	if ev.ReportKind != "unknown" || ev.SourceReferenceCount == nil || *ev.SourceReferenceCount != 1 || len(ev.SourceReferences) != 1 || ev.SourceReferences[0].Kind != "declared_source" {
		t.Fatalf("claim promoted or alias count inflated: %+v", ev)
	}
	if ev.SourcePublishedAt == nil || ev.SourcePublishedAt.Format(time.RFC3339) != "2026-11-11T08:00:00Z" || ev.SourcePublishedAtBasis != "provided" || ev.ObservedAtBasis != "provided" || !ev.ObservedAt.Equal(observed) {
		t.Fatalf("explicit dates lost: %+v", ev)
	}
	r.SubmissionProvenance.Client.Transport = "web"
	r.SubmissionProvenance.Client.NameSource = ""
	ev = r.publicEvidence()
	if stringValue(ev.SubmissionTransport) != "web" || ev.SubmissionClient != nil || ev.SubmissionClientClaimed {
		t.Fatalf("unmarked client trusted: %+v", ev)
	}
}

func TestRelayedAgentRetainsAcquisitionAndAuthoritativeSourceTime(t *testing.T) {
	indexed := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", ExternalSource: true, SourcePlatform: "twitter", PublishedAt: &indexed,
		SubmissionProvenance: retainedSubmission(t, `{"client":{"transport":"mcp"},"published_at":"2026-11-11T10:00:00Z","source_url":"https://x.com/person/status/1234567890123456789"}`)}
	ev := r.publicEvidence()
	if ev.IntakeMethod != "agent_submission" || ev.IntakeLabel != "Agent-assisted submission" || stringValue(ev.SubmissionTransport) != "mcp" || stringValue(ev.SourcePlatform) != "twitter" {
		t.Fatalf("original source confused with acquisition: %+v", ev)
	}
	if ev.SourcePublishedAt == nil || !ev.SourcePublishedAt.Equal(indexed) || ev.SourcePublishedAtBasis != "source_metadata" {
		t.Fatalf("caller date overwrote indexed original: %+v", ev)
	}
	r.ExternalSource, r.SourcePlatform = false, "web"
	r.SubmissionProvenance.Platform = "Supplied source"
	if got := stringValue(r.publicEvidence().SourcePlatform); got != "Supplied source" {
		t.Fatalf("explicit source context lost: %q", got)
	}
}

func TestPublicFacadeHasRecordedAgentAcquisitionWithoutInventingClient(t *testing.T) {
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "public_agent", GenerationMethod: "agent_submission"}
	ev := r.publicEvidence()
	if ev.IntakeMethod != "agent_submission" || stringValue(ev.IntakeChannel) != "agent_api" || stringValue(ev.SubmissionTransport) != "agent_api" || ev.SubmissionClient != nil {
		t.Fatalf("recorded public intake misrepresented: %+v", ev)
	}
}

func TestLinkedXPostTimeIsExplicitlyDerivedAndNeverBecomesObservationTime(t *testing.T) {
	now := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	posted := now.Add(-time.Hour)
	id := strconv.FormatUint(uint64(posted.UnixMilli()-1288834974657)<<22, 10)
	u := "https://x.com/person/status/" + id
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", SubmissionProvenance: &publicSubmissionProvenance{Description: "Relayed reference: " + u}}
	ev := r.publicEvidence()
	if ev.SourcePublishedAt == nil || !ev.SourcePublishedAt.Equal(posted) || ev.SourcePublishedAtBasis != "derived_x_status_id" || ev.ObservedAt != nil {
		t.Fatalf("URL time claimed as observation: %+v", ev)
	}
	if len(ev.SourceReferences) != 1 || ev.SourceReferences[0].Kind != "linked_post" || ev.SourceReferences[0].Platform != "twitter" || ev.SourceReferenceCount == nil || *ev.SourceReferenceCount != 1 {
		t.Fatalf("lost literal public post: %+v", ev)
	}
	futureID := strconv.FormatUint(uint64(now.Add(48*time.Hour).UnixMilli()-1288834974657)<<22, 10)
	if xPostURLTime("https://x.com/person/status/"+futureID, now) != nil || xPostURLTime("https://x.com/account/access", now) != nil || xPostURLTime("https://not-x.example/person/status/"+id, now) != nil {
		t.Fatal("non-source/future URL yielded a timestamp")
	}
}

func TestPublicProvenanceRejectsSecretsAndPrivateChatsBeforeStripping(t *testing.T) {
	for _, raw := range []string{"https://x.com/person/status/123?token=short", "https://x.com/person/status/123#tracking_token=short", "https://x.com/messages/123", "https://grok.com/c/private-conversation", "https://grok.com/chat/private-conversation", "https://x.com/i/grok?conversation=123", "https://example.com/?password=short", "https://example.com/user/person%40example.com", "http://127.0.0.1/a", "https://name:password@example.com/a"} {
		if got := publicProvenanceURL(raw); got != "" {
			t.Errorf("unsafe URL retained %q => %q", raw, got)
		}
	}
	r := evidenceRecord{PrivacyRestricted: true, ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", SourceURL: "https://example.com/source", SubmissionProvenance: retainedSubmission(t, `{"product":"Known product","client":{"transport":"mcp","name":"grok","name_source":"caller_provided"},"description":"https://example.com/source"}`)}
	ev := r.publicEvidence()
	if ev.ReportedProductOrSite != nil || ev.SubmissionClient != nil || len(ev.ReferencedURLs) > 0 || len(ev.SourceReferences) > 0 || ev.SourceReferenceCount != nil || ev.SourceURL != nil {
		t.Fatalf("private context exposed: %+v", ev)
	}
	r.PrivacyRestricted = false
	r.SubmissionProvenance = retainedSubmission(t, `{"product":"Call +1 415 555 0123","source_url":"https://example.com/?token=private"}`)
	ev = r.publicEvidence()
	if ev.ReportedProductOrSite != nil || ev.SourceReferenceCount != nil || len(ev.SourceReferences) != 0 {
		t.Fatalf("sensitive fields exposed or withheld references counted as zero: %+v", ev)
	}
}

func TestReferenceLimitsDoNotClaimTruncatedListsAreComplete(t *testing.T) {
	p := &publicSubmissionProvenance{DescriptionStatus: "omitted"}
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", SubmissionProvenance: p}
	if r.publicEvidence().SourceReferenceCount != nil {
		t.Fatal("omitted original falsely counted as no references")
	}
	p.DescriptionStatus = "available"
	for i := 0; i < 34; i++ {
		p.Upstream = append(p.Upstream, struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{"url", fmt.Sprintf("https://example.com/ref/%d", i)})
	}
	ev := r.publicEvidence()
	if len(ev.SourceReferences) != 32 || ev.SourceReferenceCount == nil || *ev.SourceReferenceCount != 34 {
		t.Fatalf("count truncated to display cap: %+v", ev)
	}
}

func TestSelectedProvenanceReadBindsCanonicalFetcherAndRejectsOutOfScopeRows(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(fmt.Sprint(outside), func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			seq := 7
			if outside {
				seq = 8
			}
			m.ExpectQuery(`(?s)WITH selected AS.*ORDER BY w\.submitted_at DESC,w\.submission_id DESC.*rr\.fetcher_id=w\.fetcher_id.*w\.report_seq IN \(\?\).*visibility.*MAX_EXECUTION_TIME\(3000\).*OCTET_LENGTH\(metadata\)<=65536`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"report_seq", "metadata"}).AddRow(seq, `{"product":"Product","description":"https://example.com/problem","description_status":"available"}`))
			records := []evidenceRecord{{Seq: 7, ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted"}}
			err = hydratePublicSubmissionProvenance(context.Background(), db, records)
			if outside && err == nil {
				t.Fatal("out-of-scope row accepted")
			}
			if !outside && (err != nil || records[0].SubmissionProvenance == nil || records[0].SubmissionProvenance.Product != "Product") {
				t.Fatalf("metadata not retained: %v %+v", err, records)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSubmissionMetadataSQLDoesNotExposeRawBodyOrUseClaimedAgentIdentity(t *testing.T) {
	// Public JSON must contain only the declared projection, not the private DTO.
	r := evidenceRecord{ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", SubmissionProvenance: &publicSubmissionProvenance{Product: "Product", Description: "PRIVATE ORIGINAL BODY"}}
	data, err := json.Marshal(r.publicEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE ORIGINAL BODY") || regexp.MustCompile(`"agent_name"|"description"`).Match(data) {
		t.Fatal("original/agent body leaked")
	}
}

func TestLateCanonicalPrivacyRestrictionScrubsLoadedPublicDerivatives(t *testing.T) {
	for _, flag := range []string{"contains_pii", "requires_redaction"} {
		t.Run(flag, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			const sentinel = "LATE_PRIVATE_DERIVATIVE_SENTINEL"
			r := evidenceRecord{Seq: 71, ActorKind: "machine", IntakeChannel: "wire", GenerationMethod: "human_assisted", Title: sentinel, Summary: sentinel, BrandDisplay: sentinel, Excerpt: sentinel, Impact: sentinel, Reason: sentinel, Version: 1, SourceURL: "https://example.com/" + sentinel, HasOriginalMedia: true, SupportingSourcesJSON: []byte(`[{"title":"` + sentinel + `","source_url":"https://example.com/` + sentinel + `"}]`)}
			mock.ExpectQuery(`(?s)WITH selected AS.*`).WithArgs(71).WillReturnRows(sqlmock.NewRows([]string{"report_seq", "metadata"}).AddRow(71, `{"`+flag+`":true}`))
			records := []evidenceRecord{r}
			if err := hydratePublicSubmissionProvenance(context.Background(), db, records); err != nil {
				t.Fatal(err)
			}
			r = records[0]
			if !r.PrivacyRestricted || r.Title != "Reported issue" || r.Summary != "" || r.BrandDisplay != "" {
				t.Fatalf("late canonical flag left loaded display intact: %+v", r)
			}
			// Projected, bulk and legacy brand readers all serialize analysis
			// through this shared privacy boundary after the selected raw read.
			analysis, ev, rec := r.publicMinimalAnalysis(), r.publicEvidence(), r.publicRecurrence()
			brand := models.ReportWithMinimalAnalysis{Analysis: []models.MinimalAnalysis{analysis}, Evidence: &ev, Recurrence: &rec}
			preview := publicBrandPreviewReport(r)
			detail := &models.ReportWithAnalysis{Report: models.Report{Seq: 71}, Analysis: []models.ReportAnalysis{{Title: sentinel, Description: sentinel, Summary: sentinel, BrandDisplayName: sentinel, AnalysisImage: []byte(sentinel)}}}
			attachPublicDetailProjection(detail, r, true)
			for name, output := range map[string]any{"brand": brand, "preview": preview, "detail": detail} {
				data, err := json.Marshal(output)
				if err != nil || strings.Contains(string(data), sentinel) {
					t.Fatalf("%s leaked a derivative after late restriction: %v %s", name, err, data)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
