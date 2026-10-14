package database

import (
	"strings"
	"testing"
	"time"
)

func TestEvidenceFiltersAreStrictAndScopeIsExplicit(t *testing.T) {
	for _, kind := range []string{"firsthand", "coverage", "unknown", "all"} {
		filter, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: " Anthropic ", Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		query, args := evidenceBrandCTE(filter)
		if strings.Contains(query, "LIKE") {
			t.Fatal("brand scope must not use substring matching")
		}
		if !strings.Contains(query, "rr.visibility") || !strings.Contains(query, "ro.is_public") || !strings.Contains(query, "rs.status") {
			t.Fatal("public/privacy guard missing")
		}
		if len(args) < 4 {
			t.Fatalf("scope args: %v", args)
		}
	}
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic"})
	if filter.Kind != "firsthand" || filter.Sort != "signal" {
		t.Fatalf("defaults = %+v", filter)
	}
	if got := strings.Join(filter.Brands, ","); got != "anthropic,claude,claude-code,claudecode" {
		t.Fatalf("scope=%s", got)
	}
	if _, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Product: "openai"}); err == nil {
		t.Fatal("foreign product accepted")
	}
	if _, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "openai", Kind: "bogus"}); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestEvidenceQueryCountAndPageShareCanonicalUniverse(t *testing.T) {
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "openai", Kind: "firsthand", Sort: "recurrence"})
	cte, _ := evidenceBrandCTE(filter)
	if !strings.Contains(cte, "PARTITION BY source_key") || !strings.Contains(cte, "evidence_version > 0") {
		t.Fatal("source dedup/enrichment gating missing")
	}
	if !strings.Contains(cte, "ROW_NUMBER() OVER (PARTITION BY ra.seq") {
		t.Fatal("translations counted separately")
	}
	order := evidenceOrderBy(filter.Sort)
	if !strings.Contains(order, "seq DESC") || !strings.Contains(order, "related_observation_count DESC") {
		t.Fatal("unstable recurrence paging")
	}
	if !strings.Contains(evidenceKindWhere("firsthand"), "report_kind = ?") {
		t.Fatal("firsthand must not admit unknown")
	}
	if evidenceKindWhere("all") != "1 = 1" {
		t.Fatal("all must retain historical coverage")
	}
}

func TestEvidenceCaseMappingUsesTheDedupWinnerAndIndexedPrivacyLookup(t *testing.T) {
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic"})
	query, _ := evidenceBrandCTE(filter)
	order := "PARTITION BY source_key ORDER BY evidence_version DESC, evidence_strength DESC, entity_relevance DESC, seq DESC"
	if !strings.Contains(query, "ROW_NUMBER() OVER ("+order+") AS duplicate_rank") || !strings.Contains(query, "FIRST_VALUE(seq) OVER ("+order+") AS representative_seq") {
		t.Fatal("case membership must follow the same canonical winner as duplicate ranking")
	}
	if !strings.Contains(query, "JOIN representatives n ON n.seq = cr.seq GROUP BY n.representative_seq") || strings.Contains(query, "e.source_key = n.source_key") {
		t.Fatal("case mapping must use integer report sequences without a computed-key scope cross join")
	}
	if !strings.Contains(query, "(SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq = r.seq AND ro.owner IS NOT NULL AND ro.owner <> '' AND ro.is_public = FALSE) = 0") {
		t.Fatal("privacy lookup must remain correlated by indexed sequence and reject every identified private owner")
	}
}

func TestEvidenceMetadataProjectionKeepsUnprojectedHistory(t *testing.T) {
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic"})
	query, _ := evidenceBrandCTE(filter)
	if !strings.Contains(query, "LEFT JOIN report_metadata_projection m ON m.seq=ra.seq") || !strings.Contains(query, "LEFT JOIN reports fallback ON fallback.seq=CASE WHEN m.seq IS NULL THEN ra.seq ELSE NULL END") {
		t.Fatal("historical fallback must use a null lookup key for already projected reports")
	}
	for _, field := range []string{"id", "public_id", "ts", "latitude", "longitude"} {
		if !strings.Contains(query, "CASE WHEN m.seq IS NOT NULL THEN m."+field+" ELSE fallback."+field+" END AS "+field) {
			t.Fatalf("projection must preserve nullable metadata without coalescing stale fallback values: %s", field)
		}
	}
	order := "PARTITION BY group_key ORDER BY evidence_strength DESC, entity_relevance DESC, severity_level DESC, COALESCE(observed_at,source_timestamp,ts) DESC,seq DESC"
	if !strings.Contains(query, "ROW_NUMBER() OVER ("+order+") AS family_rank") || !strings.Contains(query, "FIRST_VALUE(seq) OVER ("+order+") AS family_representative_seq") {
		t.Fatal("family aggregate mapping must follow its exact ranking representative")
	}
	if strings.Contains(query, "fs.group_key=f.group_key") || !strings.Contains(query, "fs.family_representative_seq=f.family_representative_seq") {
		t.Fatal("family aggregates must join on integer representatives")
	}
}

func TestEvidenceSafeSourcesAndOriginalDates(t *testing.T) {
	if safeEvidenceURL("javascript:alert(1)") != "" || safeEvidenceURL("https://user:secret@example.com/a") != "" || safeEvidenceURL("http://localhost/a") != "" {
		t.Fatal("unsafe public source admitted")
	}
	if got := safeEvidenceURL("https://GitHub.com/org/repo/issues/1#comment"); got != "https://github.com/org/repo/issues/1" {
		t.Fatalf("canonical source=%s", got)
	}
	added := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	published := added.AddDate(0, -3, 0)
	row := evidenceRecord{IngestedAt: added, PublishedAt: &published, Kind: "firsthand", Version: 1, SourceURL: "https://github.com/org/repo/issues/1", Excerpt: "My workflow fails.", ActorKind: "machine"}
	ev := row.publicEvidence()
	if ev.SourcePublishedAt == nil || !ev.SourcePublishedAt.Equal(published) || !ev.IngestedAt.Equal(added) || ev.ObservedAt != nil {
		t.Fatal("source/observed/ingest timestamps conflated")
	}
	if ev.IntakeMethod != "imported" || ev.ValidationStatus != "not_independently_reproduced" || ev.ResolutionStatus != "unknown" {
		t.Fatalf("unsupported trust claim=%+v", ev)
	}
	row.PrivacyRestricted = true
	ev = row.publicEvidence()
	if ev.Excerpt != nil {
		t.Fatal("private excerpt exposed")
	}
	row.Version = 0
	ev = row.publicEvidence()
	if ev.ReportKind != "unknown" {
		t.Fatal("unenriched record called firsthand")
	}
}

func TestEvidenceDoesNotRepublishCredentialsOrPersonalContacts(t *testing.T) {
	for _, quote := range []string{"Logs: api_key=" + "sk-" + strings.Repeat("x", 16), "Contact me at person@example.com", "Authorization: Bearer abcdefghijklmnop", "I fail\x1b[123"} {
		row := evidenceRecord{Version: 1, Kind: "firsthand", Excerpt: quote}
		if row.publicEvidence().Excerpt != nil {
			t.Fatalf("sensitive literal source excerpt exposed: %q", quote)
		}
	}
	row := evidenceRecord{Version: 1, Kind: "firsthand", Excerpt: "My API key settings page fails to open."}
	if row.publicEvidence().Excerpt == nil {
		t.Fatal("ordinary evidence omitted")
	}
}

func TestHumanAuthoredImportIsNotACleanAppSubmission(t *testing.T) {
	imported := evidenceRecord{Version: 1, Kind: "firsthand", ActorKind: "human", SourcePlatform: "bluesky", ExternalSource: true, GenerationMethod: "human_authored"}
	if got := imported.publicEvidence(); got.IntakeMethod != "imported" {
		t.Fatalf("import provenance changed to human submission: %+v", got)
	}
	direct := evidenceRecord{Version: 1, Kind: "firsthand", ActorKind: "human", GenerationMethod: "human_submission", IntakeChannel: "web"}
	if got := direct.publicEvidence(); got.IntakeMethod != "cleanapp_submission" {
		t.Fatalf("known direct intake omitted: %+v", got)
	}
	unknown := evidenceRecord{Version: 1, Kind: "firsthand", ActorKind: "human"}
	if got := unknown.publicEvidence(); got.IntakeMethod == "cleanapp_submission" {
		t.Fatal("actor alone cannot prove intake")
	}
}

func TestRelatedFamilyKeepsIndependentCorroborationUnknown(t *testing.T) {
	caseID := "case-1"
	accounts := 2
	row := evidenceRecord{GroupKey: "family:case-1", CaseID: &caseID, SupportingCount: 3, RelatedCount: 3, SourceAccountCount: &accounts, FamilyDuplicates: 4, SupportingSourcesJSON: []byte(`[{"title":"Specific failure","source_url":"https://github.com/org/repo/issues/1","report_kind":"firsthand","source_published_at":"2026-02-03T00:00:00Z"}]`)}
	recurrence := row.publicRecurrence()
	if recurrence.Grouping != "related_issue_family" || recurrence.SupportingReportCount != 3 || recurrence.SourceAccountCount == nil || *recurrence.SourceAccountCount != 2 || recurrence.Independence != "unassessed" || recurrence.DuplicateImportCount != 4 {
		t.Fatalf("untruthful family evidence: %+v", recurrence)
	}
	if len(recurrence.SupportingSources) != 1 {
		t.Fatal("family cannot expand to original evidence")
	}
	row.SupportingSourcesJSON = []byte(`[{"title":"Unsafe","source_url":"javascript:alert(1)","report_kind":"unknown","source_published_at":null}]`)
	if row.publicRecurrence().SupportingSources[0].SourceURL != nil {
		t.Fatal("unsafe related link exposed")
	}
}

func TestNativeSharePreservesActualCleanAppIntakeAndWireCannotSpoofIt(t *testing.T) {
	shared := evidenceRecord{Version: 1, Kind: "coverage", ActorKind: "human", IntakeChannel: "mobile", GenerationMethod: "shared_submission", SourcePlatform: "twitter", ExternalSource: true}
	if shared.publicEvidence().IntakeMethod != "cleanapp_submission" {
		t.Fatal("native shared submission became imported solely because its original source was external")
	}
	shared.IntakeChannel = "wire"
	if shared.publicEvidence().IntakeMethod == "cleanapp_submission" {
		t.Fatal("Wire generation_method spoofed authenticated native intake")
	}
}

func TestSignalRankKeepsEvidenceAndRelevanceAheadOfFamilySize(t *testing.T) {
	order := evidenceOrderBy("signal")
	expected := []string{"evidence_strength DESC", "entity_relevance DESC", "supporting_report_count DESC", "severity_level DESC", "last_signal_at DESC", "seq DESC"}
	previous := -1
	for _, criterion := range expected {
		position := strings.Index(order, criterion)
		if position <= previous {
			t.Fatalf("signal order must preserve evidence/relevance before family size: %s", order)
		}
		previous = position
	}
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "claude"})
	query, _ := evidenceBrandCTE(filter)
	if !strings.Contains(query, "WHEN entity_relationship='affected' THEN 2 WHEN entity_relationship='dependency' THEN 1 ELSE 0 END AS entity_relevance") {
		t.Fatal("entity relevance must favor affected, then dependency, with mentioned/unknown tied")
	}
	if !strings.Contains(query, "PARTITION BY group_key ORDER BY evidence_strength DESC, entity_relevance DESC, severity_level DESC") {
		t.Fatal("family representative selection does not share relevance precedence")
	}
	if strings.Contains(order, "independent") {
		t.Fatal("related source observations cannot become independent corroboration")
	}
}
