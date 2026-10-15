package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"report-listener/models"
)

func TestPublicDetailPrivacyCoversLegacyAndGeneratedFields(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		original := "private-original-description"
		report := &models.ReportWithAnalysis{
			Report:   models.Report{Seq: 1, Image: []byte("private-original-image"), Description: &original},
			Analysis: []models.ReportAnalysis{{Title: "private-ai-title", Summary: "private-ai-summary", Description: "private-ai-description", AnalysisImage: []byte("private-ai-image"), AnalysisText: "private-internal-text"}},
		}
		record := evidenceRecord{Seq: 1, Version: 1, Kind: "firsthand", SourceURL: "https://example.test/original", Excerpt: "private-original-excerpt", HasOriginalMedia: true, PrivacyRestricted: restricted}
		attachPublicDetailProjection(report, record, true)
		if !restricted {
			if report.Analysis[0].Title != "private-ai-title" || report.Report.Description == nil || len(report.Report.Image) == 0 || !report.Evidence.HasOriginalMedia || report.Evidence.SourceURL == nil {
				t.Fatal("public projection unexpectedly changed unrestricted report content")
			}
			continue
		}
		encoded, err := json.Marshal(report)
		if err != nil || strings.Contains(string(encoded), "private-") || report.Report.Image != nil || report.Analysis[0].AnalysisImage != nil || report.Analysis[0].AnalysisText != "" {
			t.Fatalf("restricted raw or AI-derived content escaped: %s %v", encoded, err)
		}
		if report.Analysis[0].Title != "Reported issue" || report.Evidence.SourceURL != nil || report.Evidence.HasOriginalMedia || report.Evidence.Excerpt != nil {
			t.Fatal("privacy restriction did not cover legacy fields and evidence together")
		}
	}
}

func TestPublicDetailMissingProjectionFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("WITH preferred AS").WithArgs("en", 1).WillReturnRows(sqlmock.NewRows([]string{"seq"}))
	if err := (&Database{db: db}).AttachPublicEvidence(context.Background(), []*models.ReportWithAnalysis{{Report: models.Report{Seq: 1}}}); err == nil {
		t.Fatal("missing public projection authorized raw report serialization")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
