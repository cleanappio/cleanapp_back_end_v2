package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEvidenceBrandSnapshotReturnsScopedFamilyCountsAndHistoricalDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	filter, _ := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand", Sort: "recent", Limit: 1, Offset: 1})
	query, args := evidenceBrandPageQuery(filter)
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg
	}
	mock.ExpectBegin()
	columns := strings.Split("all,firsthand,coverage,unknown,total,high,medium,total_reports,reports_last_30_days,reports_last_7_days,reports_previous_7_days,latest_report_at,seq,public_id,ts,latitude,longitude,title,summary,classification,severity_level,language,brand_name,brand_display_name,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version,source_platform,source_url,source_timestamp,observed_at,actor_kind,fetcher_id,privacy_restricted,source_key,import_count,case_id,related_observation_count,has_original_media,external_source,intake_channel,generation_method,source_account_key,group_key,supporting_report_count,source_account_count,duplicate_import_count,supporting_sources,latest_observed_at,latest_source_published_at,ingestion_time_missing,ingested_at_null", ",")
	added := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	published := added.AddDate(0, -4, 0)
	rows := sqlmock.NewRows(columns).AddRow(5, 3, 2, 1, 3, 1, 1, 9, 7, 4, 2, added, 42, "r_test", added, 0, 0, "History loss", "Generated summary", "digital", .7, "en", "claude", "Claude", "firsthand", "I lost the conversation history.", "Conversation history lost", "Source describes their own workflow", "affected", 1, "github_issue", "https://github.com/org/repo/issues/1", published, nil, "machine", "fetcher", false, "url:github.com/org/repo/issues/1", 3, "case-1", 2, false, true, "wire", "legacy_v3_bulk_ingest", "github:user", "family:case-1", 2, 1, 2, `[{"title":"History loss","source_url":"https://github.com/org/repo/issues/1","report_kind":"firsthand","source_published_at":"2026-06-14T00:00:00Z"}]`, nil, published, false, false)
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(values...).WillReturnRows(rows)
	mock.ExpectCommit()
	page, err := (&Database{db: db}).getEvidenceBrandPageSQL(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if page.Overview.TotalReports != 9 || page.Overview.ReportsLast30Days != 7 || page.Overview.ReportsLast7Days != 4 || page.Overview.ReportsPrevious7Days != 2 || page.Overview.LatestReportAt == nil || !page.Overview.LatestReportAt.Equal(added) {
		t.Fatalf("overview lost raw record totals: %+v", page.Overview)
	}
	if len(page.Reports) != 1 || page.Total != 3 || page.KindCounts.All != 5 || page.KindCounts.Unknown != 1 || !page.HasMore {
		t.Fatalf("counts/paging = %+v", page)
	}
	item := page.Reports[0]
	if item.Evidence.SourcePublishedAt == nil || !item.Evidence.SourcePublishedAt.Equal(published) || item.Evidence.ObservedAt != nil || !item.Evidence.IngestedAt.Equal(added) {
		t.Fatalf("historical dates = %+v", item.Evidence)
	}
	if item.Recurrence.Grouping != "related_issue_family" || item.Recurrence.SupportingReportCount != 2 || item.Recurrence.Independence != "unassessed" {
		t.Fatalf("family recurrence = %+v", item.Recurrence)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceCountFailureDoesNotSubstituteCorpusOrPartialCounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("WITH preferred").WillReturnError(errors.New("deadline exceeded"))
	mock.ExpectRollback()
	_, err = (&Database{db: db}).getEvidenceBrandPageSQL(context.Background(), EvidenceFilter{BrandName: "openai", Kind: "unknown"})
	if err == nil {
		t.Fatal("failed counts returned apparently exact page")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
