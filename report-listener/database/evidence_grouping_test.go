package database

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"report-listener/models"
)

func groupingDate(day int) *time.Time {
	d := time.Date(2026, time.October, day, 0, 0, 0, 0, time.UTC)
	return &d
}

func groupingRecord(seq int, source, kind string) evidenceRecord {
	return evidenceRecord{Seq: seq, SourceKey: source, Kind: kind, Version: 1, Excerpt: "original", Relationship: "affected", Severity: 0.5, IngestedAt: *groupingDate(1), Title: fmt.Sprintf("Title %d", seq)}
}

// Fixture IDs use the generated lowercase-hex domain, so this constructs the
// same priorities that SQL ORDER BY case_id supplies. Production code never
// approximates database collation with Go lexical string ordering.
func groupingCases(ids map[int]string) map[int]evidenceCaseReference {
	distinct := map[string]struct{}{}
	for _, id := range ids {
		distinct[id] = struct{}{}
	}
	ordered := make([]string, 0, len(distinct))
	for id := range distinct {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	ranks := map[string]int{}
	for rank, id := range ordered {
		ranks[id] = rank
	}
	refs := map[int]evidenceCaseReference{}
	for seq, id := range ids {
		refs[seq] = evidenceCaseReference{ID: id, Order: ranks[id]}
	}
	return refs
}

func groupingSeqs(page evidenceGroupedPage) []int {
	out := make([]int, len(page.Records))
	for i, record := range page.Records {
		out[i] = record.Seq
	}
	return out
}

func TestEvidenceGroupingDuplicateWinnerAndCaseLinksUseAllImportsBeforeFilter(t *testing.T) {
	old := groupingRecord(1, "source:one", "coverage")
	old.Version = 1
	winner := groupingRecord(2, "source:one", "firsthand")
	winner.Version = 2
	winner.Impact = "work blocked"
	other := groupingRecord(3, "source:two", "firsthand")
	other.Impact = "work blocked"
	caseIDs := groupingCases(map[int]string{1: "case_000000000000000000000001", 2: "case_000000000000000000000002", 3: "case_000000000000000000000001"})
	page := groupEvidenceRecords([]evidenceRecord{winner, other, old}, caseIDs, EvidenceFilter{Kind: "firsthand", Limit: 12})
	if page.Total != 1 || len(page.Records) != 1 || page.Records[0].Seq != 3 {
		t.Fatalf("canonical-source case mapping failed: %+v", page)
	}
	r := page.Records[0]
	if r.CaseID == nil || *r.CaseID != caseIDs[1].ID || r.GroupKey != "family:"+caseIDs[1].ID || r.SupportingCount != 2 || r.FamilyDuplicates != 1 || r.RelatedCount != 2 {
		t.Fatalf("imports inflated recurrence or lost an older import's case: %+v", r)
	}
	if page.KindCounts.All != 1 || page.KindCounts.Firsthand != 1 || page.KindCounts.Coverage != 0 {
		t.Fatalf("facets counted a losing duplicate: %+v", page.KindCounts)
	}
	if r.SourceAccountCount != nil {
		t.Fatal("missing source accounts became a known people count")
	}
}

func TestEvidenceGroupingSourceWinnerOrderIsVersionStrengthRelevanceSequence(t *testing.T) {
	rows := []evidenceRecord{}
	for i := 1; i <= 5; i++ {
		r := groupingRecord(i, "same", "firsthand")
		r.Relationship = "dependency"
		rows = append(rows, r)
	}
	rows[0].Version = 2
	rows[0].Excerpt = ""
	rows[1].Version = 2
	rows[1].Impact = "impact"
	rows[2].Version = 2
	rows[2].Impact = "impact"
	rows[2].Relationship = "affected"
	rows[3] = rows[2]
	rows[3].Seq = 4
	rows[4].Version = 1
	rows[4].Severity = 1
	page := groupEvidenceRecords(rows, nil, EvidenceFilter{Kind: "all", Limit: 12})
	if !reflect.DeepEqual(groupingSeqs(page), []int{4}) || page.Records[0].ImportCount != 5 || page.Records[0].FamilyDuplicates != 4 {
		t.Fatalf("source winner or duplicate counts changed: %+v", page)
	}
	rows[3].PrivacyRestricted = true
	if got := groupingSeqs(groupEvidenceRecords(rows, nil, EvidenceFilter{Kind: "all", Limit: 12})); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("private excerpt retained evidence strength: %v", got)
	}
}

func TestEvidenceGroupingFacetsOverlapAndFilterBeforeFamilyRanking(t *testing.T) {
	a := groupingRecord(1, "a", "firsthand")
	b := groupingRecord(2, "b", "coverage")
	c := groupingRecord(3, "c", "unknown")
	b.Impact = "impact"
	caseIDs := groupingCases(map[int]string{1: "case_000000000000000000000001", 2: "case_000000000000000000000001"})
	for kind, wanted := range map[string][]int{"firsthand": {1}, "coverage": {2}, "unknown": {3}, "all": {2, 3}} {
		page := groupEvidenceRecords([]evidenceRecord{a, b, c}, caseIDs, EvidenceFilter{Kind: kind, Limit: 12})
		if !reflect.DeepEqual(groupingSeqs(page), wanted) || page.KindCounts != (models.EvidenceKindCounts{All: 2, Firsthand: 1, Coverage: 1, Unknown: 1}) {
			t.Fatalf("kind%s changed facets or filtered winner: seqs=%v counts=%+v", kind, groupingSeqs(page), page.KindCounts)
		}
		if kind != "all" && page.Records[0].SupportingCount != 1 {
			t.Fatalf("kind%s recurrence included another kind", kind)
		}
	}
}

func TestEvidenceGroupingSupportingSamplesLimitBeforePrivacyExclusion(t *testing.T) {
	rows := []evidenceRecord{}
	caseIDs := map[int]evidenceCaseReference{}
	for i := 1; i <= 10; i++ {
		r := groupingRecord(i, fmt.Sprintf("source:%d", i), "firsthand")
		r.Excerpt = ""
		r.SourceURL = fmt.Sprintf("https://example.test/%d", i)
		r.PrivacyRestricted = i >= 4
		rows = append(rows, r)
		caseIDs[i] = evidenceCaseReference{ID: "case_000000000000000000000001"}
	}
	page := groupEvidenceRecords(rows, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	var samples []models.PublicSupportingSource
	if err := json.Unmarshal(page.Records[0].SupportingSourcesJSON, &samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || page.Records[0].SupportingCount != 10 || samples[0].Title != "Title 3" {
		t.Fatalf("private samples or family counts changed: %+v", samples)
	}
	for i := range rows {
		rows[i].PrivacyRestricted = true
	}
	page = groupEvidenceRecords(rows, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	if string(page.Records[0].SupportingSourcesJSON) != "[]" || page.Total != 1 || page.Records[0].SupportingCount != 10 {
		t.Fatal("all-private source samples removed their family or were not an empty array")
	}
}

func TestEvidenceGroupingDatesAccountsAndFamilyRepresentativeUseIndividualSignalDate(t *testing.T) {
	a := groupingRecord(1, "a", "firsthand")
	a.ObservedAt = groupingDate(2)
	a.PublishedAt = groupingDate(10)
	a.IngestedAt = *groupingDate(11)
	a.SourceAccountKey = "platform:account"
	b := groupingRecord(2, "b", "firsthand")
	b.PublishedAt = groupingDate(3)
	b.SourceAccountKey = "platform:account"
	c := groupingRecord(3, "c", "firsthand")
	c.IngestedAt = *groupingDate(4)
	c.SourceAccountKey = "platform:other"
	caseIDs := groupingCases(map[int]string{1: "case_000000000000000000000001", 2: "case_000000000000000000000001", 3: "case_000000000000000000000001"})
	page := groupEvidenceRecords([]evidenceRecord{a, b, c}, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	r := page.Records[0]
	if r.Seq != 3 || r.LatestObservedAt == nil || !r.LatestObservedAt.Equal(*groupingDate(2)) || r.LatestPublishedAt == nil || !r.LatestPublishedAt.Equal(*groupingDate(10)) || r.SourceAccountCount == nil || *r.SourceAccountCount != 2 {
		t.Fatalf("dates or exact account count conflated: %+v", r)
	}
	b.SourceAccountKey = ""
	page = groupEvidenceRecords([]evidenceRecord{a, b, c}, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	if page.Records[0].SourceAccountCount != nil {
		t.Fatal("partially missing source accounts returned a known count")
	}
}

func TestEvidenceGroupingDistinguishesNullAndNonNullZeroSignalDates(t *testing.T) {
	zero := time.Time{}
	ingested := groupingRecord(1, "zero-ingested", "firsthand")
	ingested.IngestedAt = zero
	published := groupingRecord(2, "zero-published", "firsthand")
	published.PublishedAt = &zero
	observed := groupingRecord(3, "zero-observed", "firsthand")
	observed.ObservedAt = &zero
	missing := groupingRecord(9, "null-dates", "firsthand")
	missing.IngestedAt, missing.IngestedAtNull = zero, true
	for _, sortBy := range []string{"signal", "recent", "recurrence"} {
		page := groupEvidenceRecords([]evidenceRecord{missing, observed, ingested, published}, nil, EvidenceFilter{Kind: "all", Sort: sortBy, Limit: 12})
		if got := groupingSeqs(page); !reflect.DeepEqual(got, []int{3, 2, 1, 9}) {
			t.Fatalf("sort %s treated a non-NULL zero date as NULL or used a later fallback: %v", sortBy, got)
		}
	}
	// All three COALESCE dates are NULL for the higher-sequence import. The
	// non-NULL zero observation must win and remain a non-NULL MAX result.
	known := groupingRecord(10, "family-zero", "firsthand")
	known.ObservedAt, known.PublishedAt, known.IngestedAt, known.IngestedAtNull = &zero, &zero, zero, true
	missing.Seq, missing.SourceKey = 100, "family-null"
	otherMissing := missing
	otherMissing.Seq, otherMissing.SourceKey = 20, "all-null-one"
	lastMissing := otherMissing
	lastMissing.Seq, lastMissing.SourceKey = 30, "all-null-two"
	refs := groupingCases(map[int]string{10: "case_zero", 100: "case_zero", 20: "case_null", 30: "case_null"})
	for _, sortBy := range []string{"signal", "recent", "recurrence"} {
		page := groupEvidenceRecords([]evidenceRecord{missing, lastMissing, known, otherMissing}, refs, EvidenceFilter{Kind: "all", Sort: sortBy, Limit: 12})
		if got := groupingSeqs(page); !reflect.DeepEqual(got, []int{10, 30}) {
			t.Fatalf("sort %s lost known-zero family MAX or NULL family ordering: %v", sortBy, got)
		}
		if r := page.Records[0]; r.LatestObservedAt == nil || !r.LatestObservedAt.IsZero() || r.LatestPublishedAt == nil || !r.LatestPublishedAt.IsZero() || r.SupportingCount != 2 {
			t.Fatal("non-NULL zero family dates became NULL")
		}
		if r := page.Records[1]; r.LatestObservedAt != nil || r.LatestPublishedAt != nil {
			t.Fatal("all-NULL family dates became a synthetic date")
		}
	}
}

func TestEvidenceGroupingSortsCountsAndEmptyOffset(t *testing.T) {
	a := groupingRecord(1, "a", "firsthand")
	a.Impact = "impact"
	a.Severity = 0.7
	a.IngestedAt = *groupingDate(2)
	b := groupingRecord(2, "b", "firsthand")
	b.Severity = 0.4
	b.IngestedAt = *groupingDate(4)
	c := groupingRecord(3, "c", "firsthand")
	c.Severity = 0.3
	c.IngestedAt = *groupingDate(3)
	d := groupingRecord(4, "d", "firsthand")
	d.Severity = 0.1
	d.IngestedAt = *groupingDate(1)
	caseIDs := groupingCases(map[int]string{3: "case_000000000000000000000001", 4: "case_000000000000000000000001"})
	for sortBy, wanted := range map[string][]int{"signal": {1, 3, 2}, "recent": {2, 3, 1}, "recurrence": {3, 1, 2}} {
		page := groupEvidenceRecords([]evidenceRecord{a, b, c, d}, caseIDs, EvidenceFilter{Kind: "firsthand", Sort: sortBy, Limit: 12})
		if !reflect.DeepEqual(groupingSeqs(page), wanted) || page.Total != 3 || page.High != 1 || page.Medium != 1 {
			t.Fatalf("sort%s: seqs=%v total/high/medium=%d/%d/%d", sortBy, groupingSeqs(page), page.Total, page.High, page.Medium)
		}
	}
	page := groupEvidenceRecords([]evidenceRecord{a, b, c, d}, caseIDs, EvidenceFilter{Kind: "firsthand", Limit: 1, Offset: 1})
	if !page.HasMore || !reflect.DeepEqual(groupingSeqs(page), []int{3}) {
		t.Fatal("bounded pagination lost remaining entries")
	}
	page = groupEvidenceRecords([]evidenceRecord{a, b, c, d}, caseIDs, EvidenceFilter{Kind: "firsthand", Limit: 12, Offset: 100})
	if len(page.Records) != 0 || page.Records == nil || page.Total != 3 || page.KindCounts.All != 3 || page.HasMore || page.High != 1 || page.Medium != 1 {
		t.Fatalf("empty offset lost totals/facets: %+v", page)
	}
}

func TestEvidenceGroupingNullableSeverityPreservesSQLRankingAndNumericFallback(t *testing.T) {
	zero := groupingRecord(1, "known-zero", "firsthand")
	zero.Severity, zero.IngestedAt = 0, *groupingDate(1)
	negative := groupingRecord(2, "known-negative", "firsthand")
	negative.Severity, negative.IngestedAt = -0.1, *groupingDate(2)
	missing := groupingRecord(3, "null-severity", "firsthand")
	missing.Severity, missing.SeverityNull, missing.IngestedAt = 0, true, *groupingDate(3)
	inputs := []evidenceRecord{missing, negative, zero}
	for sortBy, wanted := range map[string][]int{"signal": {1, 2, 3}, "recent": {3, 2, 1}, "recurrence": {3, 2, 1}} {
		page := groupEvidenceRecords(inputs, nil, EvidenceFilter{Kind: "all", Sort: sortBy, Limit: 12})
		if got := groupingSeqs(page); !reflect.DeepEqual(got, wanted) || page.High != 0 || page.Medium != 0 {
			t.Fatalf("sort %s changed SQL NULL severity ordering/counts: %v", sortBy, got)
		}
	}
	// Family representative selection always orders severity before its date,
	// including for recent/recurrence pages. SQL NULL follows known negatives.
	refs := groupingCases(map[int]string{1: "case_nullable", 2: "case_nullable", 3: "case_nullable"})
	for _, sortBy := range []string{"signal", "recent", "recurrence"} {
		page := groupEvidenceRecords(inputs, refs, EvidenceFilter{Kind: "all", Sort: sortBy, Limit: 12})
		if got := groupingSeqs(page); !reflect.DeepEqual(got, []int{1}) {
			t.Fatalf("sort %s chose NULL severity ahead of known zero: %v", sortBy, got)
		}
		page = groupEvidenceRecords([]evidenceRecord{negative, missing}, refs, EvidenceFilter{Kind: "all", Sort: sortBy, Limit: 12})
		if got := groupingSeqs(page); !reflect.DeepEqual(got, []int{2}) {
			t.Fatalf("sort %s chose NULL severity ahead of a known negative: %v", sortBy, got)
		}
	}
	page := groupEvidenceRecords([]evidenceRecord{missing}, nil, EvidenceFilter{Kind: "all", Limit: 12})
	if !page.Records[0].SeverityNull || page.Records[0].Severity != 0 {
		t.Fatal("grouping changed the public numeric fallback for NULL severity")
	}
}

func TestEvidenceGroupingIsDeterministicAndDoesNotModifyInput(t *testing.T) {
	rows := []evidenceRecord{groupingRecord(1, "a", "firsthand"), groupingRecord(2, "b", "firsthand")}
	before := append([]evidenceRecord(nil), rows...)
	first := groupEvidenceRecords(rows, nil, EvidenceFilter{Kind: "all", Limit: 12})
	second := groupEvidenceRecords([]evidenceRecord{rows[1], rows[0]}, nil, EvidenceFilter{Kind: "all", Limit: 12})
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(rows, before) {
		t.Fatal("map/input order changed the output or grouping mutated input records")
	}
	empty := groupEvidenceRecords(nil, nil, EvidenceFilter{Kind: "firsthand", Limit: 12})
	if empty.Records == nil || len(empty.Records) != 0 || empty.Total != 0 || empty.HasMore {
		t.Fatal("empty scope returned nil or synthetic entries")
	}
}

func TestEvidenceGroupingUsesSQLCaseOrderAndBinarySourceIdentity(t *testing.T) {
	a := groupingRecord(1, "source:Path", "firsthand")
	b := groupingRecord(2, "source:Path", "firsthand")
	c := groupingRecord(3, "source:path", "firsthand")
	// SQL's accent-insensitive ordering places ä before b, opposite to Go's
	// bytewise comparison. The supplied database priorities are authoritative.
	caseIDs := map[int]evidenceCaseReference{1: {ID: "ä", Order: 0}, 2: {ID: "b", Order: 1}}
	page := groupEvidenceRecords([]evidenceRecord{b, c, a}, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	if page.Total != 2 || page.KindCounts.All != 2 {
		t.Fatal("case-sensitive source paths collapsed")
	}
	for _, r := range page.Records {
		if r.SourceKey == "source:Path" && (r.CaseID == nil || *r.CaseID != "ä" || r.ImportCount != 2) {
			t.Fatal("case mapping approximated SQL collation or ignored a duplicate import")
		}
	}
}

func TestEvidenceGroupingStrengthUsesRawMediaPresenceAndVersion(t *testing.T) {
	media := groupingRecord(1, "same", "firsthand")
	media.Excerpt = ""
	media.HasOriginalMedia = true
	missing := groupingRecord(2, "same", "firsthand")
	missing.Version = 0
	missing.Impact = "impact"
	missing.HasOriginalMedia = true
	page := groupEvidenceRecords([]evidenceRecord{missing, media}, nil, EvidenceFilter{Kind: "all", Limit: 12})
	if page.Records[0].Seq != 1 || evidenceStrength(media) != 2 || evidenceStrength(missing) != 0 || evidenceRelevance(missing) != 0 {
		t.Fatal("raw original-media presence or missing evidence version changed ranking")
	}
	media.PrivacyRestricted = true
	media.Impact = "impact"
	if evidenceStrength(media) != 0 {
		t.Fatal("restricted media or impact produced evidence strength")
	}
	unknown := groupEvidenceRecords([]evidenceRecord{missing}, nil, EvidenceFilter{Kind: "unknown", Limit: 12})
	if unknown.Total != 1 || unknown.Records[0].Kind != "unknown" || unknown.KindCounts.Firsthand != 0 || unknown.KindCounts.Unknown != 1 {
		t.Fatal("version0 report was promoted to firsthand")
	}
}

func TestEvidenceGroupingPadSpaceIdentityPreservesWinningStrings(t *testing.T) {
	a := groupingRecord(1, "source:same", "firsthand")
	a.SourceAccountKey = "platform:alice"
	b := groupingRecord(2, "source:same ", "firsthand")
	b.SourceAccountKey = "platform:alice "
	c := groupingRecord(3, "source:other", "firsthand")
	c.SourceAccountKey = "platform:alice"
	caseIDs := map[int]evidenceCaseReference{1: {ID: "case_000000000000000000000001", Order: 0}, 3: {ID: "case_000000000000000000000001 ", Order: 1}}
	page := groupEvidenceRecords([]evidenceRecord{a, b, c}, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
	if page.Total != 1 || page.KindCounts.All != 1 || page.Records[0].SupportingCount != 2 || page.Records[0].FamilyDuplicates != 1 || page.Records[0].SourceAccountCount == nil || *page.Records[0].SourceAccountCount != 1 {
		t.Fatalf("SQL PADSPACE grouping/count equality changed: %+v", page)
	}
	if page.Records[0].GroupKey != "family:"+caseIDs[3].ID {
		t.Fatal("group equality rewrote the winning group key used by public IDs")
	}
	standalone := groupEvidenceRecords([]evidenceRecord{a, b}, nil, EvidenceFilter{Kind: "all", Limit: 12})
	if standalone.Total != 1 || standalone.Records[0].SourceKey != b.SourceKey || standalone.Records[0].GroupKey != b.SourceKey || standalone.Records[0].ImportCount != 2 {
		t.Fatal("source equality rewrote the selected original source string")
	}
	d := groupingRecord(4, "source:same\u00a0", "firsthand")
	if got := groupEvidenceRecords([]evidenceRecord{a, b, d}, nil, EvidenceFilter{Kind: "all", Limit: 12}); got.Total != 2 {
		t.Fatal("SQL binary identity was approximated with Unicode whitespace trimming")
	}
}

func BenchmarkEvidenceGrouping15000(b *testing.B) {
	rows := make([]evidenceRecord, 15000)
	caseIDs := make(map[int]evidenceCaseReference, len(rows))
	for i := range rows {
		rows[i] = groupingRecord(i+1, fmt.Sprintf("source:%d", i/2), "firsthand")
		rows[i].SourceAccountKey = fmt.Sprintf("platform:%d", i/4)
		caseIDs[i+1] = evidenceCaseReference{ID: fmt.Sprintf("case_%024x", i/20), Order: i / 20}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page := groupEvidenceRecords(rows, caseIDs, EvidenceFilter{Kind: "all", Limit: 12})
		if page.Total != 750 {
			b.Fatal("large-scope family count changed")
		}
	}
}
