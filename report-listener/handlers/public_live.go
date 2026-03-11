package handlers

import (
	"strings"
	"time"

	"report-listener/models"
)

func publicLiveClassification(analyses []models.ReportAnalysis) string {
	for _, analysis := range analyses {
		classification := strings.TrimSpace(analysis.Classification)
		if classification != "" {
			return classification
		}
	}
	return "physical"
}

func toPublicLiveAnalyses(analyses []models.ReportAnalysis) []models.PublicLiveAnalysis {
	out := make([]models.PublicLiveAnalysis, 0, len(analyses))
	for _, analysis := range analyses {
		classification := strings.TrimSpace(analysis.Classification)
		if classification == "" {
			classification = "physical"
		}
		out = append(out, models.PublicLiveAnalysis{
			SeverityLevel:    analysis.SeverityLevel,
			Classification:   classification,
			Language:         strings.TrimSpace(analysis.Language),
			Title:            strings.TrimSpace(analysis.Title),
			Summary:          strings.TrimSpace(analysis.Summary),
			BrandName:        strings.TrimSpace(analysis.BrandName),
			BrandDisplayName: strings.TrimSpace(analysis.BrandDisplayName),
		})
	}
	return out
}

func (h *Handlers) BuildPublicLiveBatch(reports []models.ReportWithAnalysis) (models.PublicLiveReportBatch, error) {
	if len(reports) == 0 {
		return models.PublicLiveReportBatch{Reports: []models.PublicLiveReport{}, Count: 0}, nil
	}

	items := make([]models.PublicLiveReport, 0, len(reports))
	for _, report := range reports {
		publicID := strings.TrimSpace(report.Report.PublicID)
		if publicID == "" {
			continue
		}

		classification := publicLiveClassification(report.Analysis)
		token, err := h.sealDiscoveryReportToken(classification, publicID)
		if err != nil {
			return models.PublicLiveReportBatch{}, err
		}

		item := models.PublicLiveReport{
			DiscoveryToken: token,
			Timestamp:      report.Report.Timestamp.UTC().Format(time.RFC3339),
			Analysis:       toPublicLiveAnalyses(report.Analysis),
		}
		if classification == "physical" {
			lat := report.Report.Latitude
			lon := report.Report.Longitude
			item.Latitude = &lat
			item.Longitude = &lon
		}
		if len(item.Analysis) == 0 {
			continue
		}
		items = append(items, item)
	}

	return models.PublicLiveReportBatch{
		Reports: items,
		Count:   len(items),
	}, nil
}
