package database

import (
	"testing"
	"time"
)

func TestEvidenceOverviewCountsReportsAndIntakeWindows(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	row := func(seq, days int) evidenceRecord {
		return evidenceRecord{Seq: seq, IngestedAt: now.AddDate(0, 0, -days)}
	}
	oldImported := row(1, 0)
	oldDate := now.AddDate(-1, 0, 0)
	oldImported.PublishedAt = &oldDate
	oldImported.ExternalSource = true
	missing := row(6, 0)
	missing.IngestedAtNull = true
	unknownArchive := row(7, 0)
	unknownArchive.IngestionTimeMissing = true
	future := row(8, -1)
	records := []evidenceRecord{oldImported, row(2, 7), row(3, 14), row(4, 30), row(5, 31), missing, unknownArchive, future, oldImported}
	overview := evidenceOverview(records, now)
	if overview.TotalReports != 8 || overview.ReportsLast30Days != 4 || overview.ReportsLast7Days != 2 || overview.ReportsPrevious7Days != 1 {
		t.Fatalf("wrong raw count/windows: %+v", overview)
	}
	if overview.LatestReportAt == nil || !overview.LatestReportAt.Equal(now) || !overview.AsOf.Equal(now) || overview.DateBasis != "added_to_cleanapp" || overview.Classification != "digital" {
		t.Fatalf("overview misrepresented clock/date basis: %+v", overview)
	}
	// An old source imported today is an addition today, never a new incident.
	if oldImported.PublishedAt.Year() != 2025 {
		t.Fatal("overview modified original publication date")
	}
	empty := evidenceOverview(nil, now)
	if empty.TotalReports != 0 || empty.ReportsLast30Days != 0 || empty.LatestReportAt != nil || !empty.AsOf.Equal(now) {
		t.Fatal("empty overview fabricated data")
	}
}

func TestRecentRankingShowsFreshReportInsteadOfOlderStrongerEvidence(t *testing.T) {
	old := groupingRecord(1, "old", "firsthand")
	old.IngestedAt = *groupingDate(15)
	published := time.Date(2025, 4, 10, 0, 0, 0, 0, time.UTC)
	old.PublishedAt = &published
	old.ExternalSource = true
	old.Impact = "blocks work"
	old.Severity = 1
	current := groupingRecord(2, "current", "firsthand")
	current.PublishedAt = groupingDate(14)
	current.ExternalSource = true
	current.Severity = .4
	rows := []evidenceRecord{old, current}
	page := groupEvidenceRecords(rows, nil, EvidenceFilter{Kind: "firsthand", Sort: "recent"})
	if got := groupingSeqs(page); len(got) != 2 || got[0] != 2 {
		t.Fatalf("new intake date made old source appear fresh: %v", got)
	}
	refs := groupingCases(map[int]string{1: "related", 2: "related"})
	page = groupEvidenceRecords(rows, refs, EvidenceFilter{Kind: "firsthand", Sort: "recent"})
	if page.Records[0].Seq != 2 || page.Records[0].SupportingCount != 2 {
		t.Fatal("recent family still displays older stronger exemplar")
	}
	for _, sortBy := range []string{"signal", "recurrence"} {
		page = groupEvidenceRecords(rows, refs, EvidenceFilter{Kind: "firsthand", Sort: sortBy})
		if page.Records[0].Seq != 1 {
			t.Fatalf("%s representative semantics changed", sortBy)
		}
	}
}
