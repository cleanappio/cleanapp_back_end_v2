package database

import (
	"time"

	"report-listener/models"
)

func evidenceOverview(records []evidenceRecord, now time.Time) models.EvidenceOverview {
	overview := models.EvidenceOverview{AsOf: now, Classification: "digital", DateBasis: "added_to_cleanapp"}
	last30, last7, previous7 := now.AddDate(0, 0, -30), now.AddDate(0, 0, -7), now.AddDate(0, 0, -14)
	seen := make(map[int]struct{}, len(records))
	for _, record := range records {
		if _, exists := seen[record.Seq]; exists {
			continue
		}
		seen[record.Seq] = struct{}{}
		overview.TotalReports++
		added := record.IngestedAt
		if record.IngestedAtNull || record.IngestionTimeMissing || added.IsZero() || added.After(now) {
			continue
		}
		overview.LatestReportAt = evidenceLatestDate(overview.LatestReportAt, &added)
		if !added.Before(last30) {
			overview.ReportsLast30Days++
		}
		if !added.Before(last7) {
			overview.ReportsLast7Days++
		} else if !added.Before(previous7) {
			overview.ReportsPrevious7Days++
		}
	}
	return overview
}
