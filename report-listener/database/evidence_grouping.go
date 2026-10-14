package database

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"report-listener/models"
)

// Order is assigned globally by SQL ORDER BY case_id using the actual database
// collation. Comparing case IDs as Go strings would change accented/custom IDs.
type evidenceCaseReference struct {
	ID    string
	Order int
}

type evidenceGroupedPage struct {
	Records             []evidenceRecord
	Total, High, Medium int
	KindCounts          models.EvidenceKindCounts
	HasMore             bool
	// Retain sequence IDs for bounded title hydration when core SQL supplied
	// only presence bits. Samples are limited before restricted rows are removed.
	SupportingSamples map[int][]evidenceRecord
}

type evidenceSourceGroup struct {
	winner, imports int
	caseReference   evidenceCaseReference
	hasCase         bool
}

type evidenceFamilyGroup struct {
	record          evidenceRecord
	members         []int
	lastSignal      time.Time
	lastSignalKnown bool
}

// groupEvidenceRecords accepts one visible, source-ranked normalized row per
// report. SQL remains authoritative for source identities, account identities,
// dates and globally ordered case references. All grouping is request-local.
func groupEvidenceRecords(records []evidenceRecord, caseIDs map[int]evidenceCaseReference, f EvidenceFilter) evidenceGroupedPage {
	page := evidenceGroupedPage{Records: []evidenceRecord{}, SupportingSamples: map[int][]evidenceRecord{}}
	sources := make(map[string]*evidenceSourceGroup, len(records))
	for i, r := range records {
		identity := evidenceIdentityKey(r.SourceKey)
		source, exists := sources[identity]
		if !exists {
			source = &evidenceSourceGroup{winner: i}
			sources[identity] = source
		} else if evidenceSourceBefore(r, records[source.winner]) {
			source.winner = i
		}
		source.imports++
		if ref, linked := caseIDs[r.Seq]; linked && (!source.hasCase || ref.Order < source.caseReference.Order) {
			source.caseReference, source.hasCase = ref, true
		}
	}
	entries := make([]evidenceRecord, 0, len(sources))
	families := make(map[string][]int, len(sources))
	facets := map[string]map[string]struct{}{"all": {}, "firsthand": {}, "coverage": {}, "unknown": {}}
	kind := f.Kind
	if kind == "" {
		kind = "firsthand"
	}
	for _, source := range sources {
		r := records[source.winner]
		if r.Version < 1 || (r.Kind != "firsthand" && r.Kind != "coverage") {
			r.Kind = "unknown"
		}
		r.ImportCount = source.imports
		r.CaseID = nil
		r.GroupKey = r.SourceKey
		if source.hasCase {
			caseID := source.caseReference.ID
			r.CaseID = &caseID
			r.GroupKey = "family:" + caseID
		}
		groupIdentity := evidenceIdentityKey(r.GroupKey)
		facets["all"][groupIdentity] = struct{}{}
		facets[r.Kind][groupIdentity] = struct{}{}
		if kind != "all" && r.Kind != kind {
			continue
		}
		families[groupIdentity] = append(families[groupIdentity], len(entries))
		entries = append(entries, r)
	}
	page.KindCounts = models.EvidenceKindCounts{All: len(facets["all"]), Firsthand: len(facets["firsthand"]), Coverage: len(facets["coverage"]), Unknown: len(facets["unknown"])}
	grouped := make([]evidenceFamilyGroup, 0, len(families))
	for _, members := range families {
		sort.Slice(members, func(i, j int) bool { return evidenceFamilyBefore(entries[members[i]], entries[members[j]]) })
		r := entries[members[0]]
		r.SupportingCount, r.RelatedCount = len(members), len(members)
		r.FamilyDuplicates = 0
		r.LatestObservedAt, r.LatestPublishedAt = nil, nil
		accounts := map[string]struct{}{}
		allAccountsKnown := true
		var lastSignal time.Time
		lastSignalKnown := false
		for _, index := range members {
			member := entries[index]
			r.FamilyDuplicates += member.ImportCount - 1
			if member.SourceAccountKey == "" {
				allAccountsKnown = false
			} else {
				accounts[evidenceIdentityKey(member.SourceAccountKey)] = struct{}{}
			}
			r.LatestObservedAt = evidenceLatestDate(r.LatestObservedAt, member.ObservedAt)
			r.LatestPublishedAt = evidenceLatestDate(r.LatestPublishedAt, member.PublishedAt)
			if signal := evidenceIndividualSignalDate(member); evidenceIndividualSignalKnown(member) && (!lastSignalKnown || signal.After(lastSignal)) {
				lastSignal = signal
				lastSignalKnown = true
			}
		}
		r.SourceAccountCount = nil
		if allAccountsKnown {
			count := len(accounts)
			r.SourceAccountCount = &count
		}
		if r.Severity >= 0.7 {
			page.High++
		} else if r.Severity >= 0.4 {
			page.Medium++
		}
		grouped = append(grouped, evidenceFamilyGroup{record: r, members: members, lastSignal: lastSignal, lastSignalKnown: lastSignalKnown})
	}
	page.Total = len(grouped)
	sort.Slice(grouped, func(i, j int) bool { return evidenceBriefingBefore(grouped[i], grouped[j], f.Sort) })
	offset := max(f.Offset, 0)
	limit := f.Limit
	if limit <= 0 {
		limit = 12
	}
	limit = min(limit, 50)
	if offset >= len(grouped) {
		return page
	}
	end := min(offset+limit, len(grouped))
	page.HasMore = end < len(grouped)
	for _, family := range grouped[offset:end] {
		r := family.record
		samples := []models.PublicSupportingSource{}
		sampleRecords := []evidenceRecord{}
		for _, index := range family.members[:min(8, len(family.members))] {
			member := entries[index]
			if member.PrivacyRestricted {
				continue
			}
			url := member.SourceURL
			samples = append(samples, models.PublicSupportingSource{Title: member.Title, SourceURL: &url, ReportKind: member.Kind, SourcePublishedAt: member.PublishedAt})
			sampleRecords = append(sampleRecords, member)
		}
		// These fields come from SQL UTF-8 and TIMESTAMP/DATETIME values, whose
		// supported date range fits the JSON time encoder.
		r.SupportingSourcesJSON, _ = json.Marshal(samples)
		page.SupportingSamples[r.Seq] = sampleRecords
		page.Records = append(page.Records, r)
	}
	return page
}

// Computed SQL identities use utf8mb4_bin, whose PADSPACE equality ignores
// trailing ASCII spaces. Preserve original winning strings for public hashes;
// this key changes only grouping/count comparisons, without case folding or
// trimming any other Unicode whitespace.
func evidenceIdentityKey(key string) string {
	return strings.TrimRight(key, " ")
}

func evidenceStrength(r evidenceRecord) int {
	if r.Version < 1 || r.PrivacyRestricted {
		return 0
	}
	strength := 0
	if r.Excerpt != "" || r.HasOriginalMedia {
		strength = 2
	}
	if r.Impact != "" {
		strength++
	}
	return strength
}

func evidenceRelevance(r evidenceRecord) int {
	if r.Version < 1 {
		return 0
	}
	switch r.Relationship {
	case "affected":
		return 2
	case "dependency":
		return 1
	default:
		return 0
	}
}

func evidenceSourceBefore(a, b evidenceRecord) bool {
	if a.Version != b.Version {
		return a.Version > b.Version
	}
	if evidenceStrength(a) != evidenceStrength(b) {
		return evidenceStrength(a) > evidenceStrength(b)
	}
	if evidenceRelevance(a) != evidenceRelevance(b) {
		return evidenceRelevance(a) > evidenceRelevance(b)
	}
	return a.Seq > b.Seq
}

func evidenceIndividualSignalDate(r evidenceRecord) time.Time {
	if r.ObservedAt != nil {
		return *r.ObservedAt
	}
	if r.PublishedAt != nil {
		return *r.PublishedAt
	}
	return r.IngestedAt
}

func evidenceIndividualSignalKnown(r evidenceRecord) bool {
	return r.ObservedAt != nil || r.PublishedAt != nil || !r.IngestedAtNull
}

func evidenceLatestDate(a, b *time.Time) *time.Time {
	if b != nil && (a == nil || b.After(*a)) {
		return b
	}
	return a
}

func evidenceFamilyBefore(a, b evidenceRecord) bool {
	if evidenceStrength(a) != evidenceStrength(b) {
		return evidenceStrength(a) > evidenceStrength(b)
	}
	if evidenceRelevance(a) != evidenceRelevance(b) {
		return evidenceRelevance(a) > evidenceRelevance(b)
	}
	if a.Severity != b.Severity {
		return a.Severity > b.Severity
	}
	if evidenceIndividualSignalKnown(a) != evidenceIndividualSignalKnown(b) {
		return evidenceIndividualSignalKnown(a)
	}
	if left, right := evidenceIndividualSignalDate(a), evidenceIndividualSignalDate(b); !left.Equal(right) {
		return left.After(right)
	}
	return a.Seq > b.Seq
}

func evidenceBriefingBefore(a, b evidenceFamilyGroup, sortBy string) bool {
	left, right := a.record, b.record
	switch sortBy {
	case "recent":
		if a.lastSignalKnown != b.lastSignalKnown {
			return a.lastSignalKnown
		}
		if !a.lastSignal.Equal(b.lastSignal) {
			return a.lastSignal.After(b.lastSignal)
		}
	case "recurrence":
		if left.RelatedCount != right.RelatedCount {
			return left.RelatedCount > right.RelatedCount
		}
		if evidenceStrength(left) != evidenceStrength(right) {
			return evidenceStrength(left) > evidenceStrength(right)
		}
		if a.lastSignalKnown != b.lastSignalKnown {
			return a.lastSignalKnown
		}
		if !a.lastSignal.Equal(b.lastSignal) {
			return a.lastSignal.After(b.lastSignal)
		}
	default:
		if evidenceStrength(left) != evidenceStrength(right) {
			return evidenceStrength(left) > evidenceStrength(right)
		}
		if evidenceRelevance(left) != evidenceRelevance(right) {
			return evidenceRelevance(left) > evidenceRelevance(right)
		}
		if left.SupportingCount != right.SupportingCount {
			return left.SupportingCount > right.SupportingCount
		}
		if left.Severity != right.Severity {
			return left.Severity > right.Severity
		}
		if a.lastSignalKnown != b.lastSignalKnown {
			return a.lastSignalKnown
		}
		if !a.lastSignal.Equal(b.lastSignal) {
			return a.lastSignal.After(b.lastSignal)
		}
	}
	return left.Seq > right.Seq
}
