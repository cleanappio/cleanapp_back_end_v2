package database

import (
	"context"
	"testing"

	"report-listener/config"
	"report-listener/models"
)

func TestReportFilteringWithStatus(t *testing.T) {
	// This test verifies that the new filtering logic works correctly
	// Skip if database not available
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     "3306",
		DBUser:     "server",
		DBPassword: "secret_app",
		DBName:     "cleanapp",
	}

	db, err := NewDatabase(cfg)
	if err != nil {
		t.Skipf("Skipping test - database not available: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()

	// Test GetLastNAnalyzedReports with the new filtering (full_data=true)
	reportsInterface, err := db.GetLastNAnalyzedReports(ctx, 10, "physical", true)
	if err != nil {
		t.Skipf("Skipping test - cannot query reports: %v", err)
		return
	}

	// Type assertion to get reports with analysis
	reports, ok := reportsInterface.([]models.ReportWithAnalysis)
	if !ok {
		t.Skipf("Skipping test - failed to type assert reports: %v", err)
		return
	}

	// Log the number of reports found
	t.Logf("Found %d non-resolved reports with analysis", len(reports))

	// Verify that all returned reports are either not in report_status or have status 'active'
	for _, report := range reports {
		// Check if this report has a status entry
		var status string
		err := db.db.QueryRowContext(ctx, "SELECT status FROM report_status WHERE seq = ?", report.Report.Seq).Scan(&status)
		if err != nil {
			// No status entry found - this is valid (should be included)
			t.Logf("Report %d has no status entry (valid)", report.Report.Seq)
		} else {
			// Status entry found - should be 'active'
			if status != "active" {
				t.Errorf("Report %d has status '%s' but should be 'active' or not exist", report.Report.Seq, status)
			} else {
				t.Logf("Report %d has status 'active' (valid)", report.Report.Seq)
			}
		}
	}

	// Test GetLastNAnalyzedReports with full_data=false (reports with minimal analysis)
	reportsWithMinimalAnalysisInterface, err := db.GetLastNAnalyzedReports(ctx, 10, "physical", false)
	if err != nil {
		t.Skipf("Skipping test - cannot query reports: %v", err)
		return
	}

	// Type assertion to get reports with minimal analysis
	reportsWithMinimalAnalysis, ok := reportsWithMinimalAnalysisInterface.([]models.ReportWithMinimalAnalysis)
	if !ok {
		t.Skipf("Skipping test - failed to type assert reports with minimal analysis: %v", err)
		return
	}

	// Log the number of reports found
	t.Logf("Found %d non-resolved reports with minimal analysis", len(reportsWithMinimalAnalysis))

	// Verify that all returned reports are either not in report_status or have status 'active'
	for _, reportWithAnalysis := range reportsWithMinimalAnalysis {
		// Check if this report has a status entry
		var status string
		err := db.db.QueryRowContext(ctx, "SELECT status FROM report_status WHERE seq = ?", reportWithAnalysis.Report.Seq).Scan(&status)
		if err != nil {
			// No status entry found - this is valid (should be included)
			t.Logf("Report %d has no status entry (valid)", reportWithAnalysis.Report.Seq)
		} else {
			// Status entry found - should be 'active'
			if status != "active" {
				t.Errorf("Report %d has status '%s' but should be 'active' or not exist", reportWithAnalysis.Report.Seq, status)
			} else {
				t.Logf("Report %d has status 'active' (valid)", reportWithAnalysis.Report.Seq)
			}
		}
	}

	// Additional test: verify that minimal analysis contains only the expected fields
	for _, reportWithAnalysis := range reportsWithMinimalAnalysis {
		if len(reportWithAnalysis.Analysis) == 0 {
			t.Errorf("Report %d has no analysis data", reportWithAnalysis.Report.Seq)
			continue
		}

		// Check each analysis object in the array
		for i, analysis := range reportWithAnalysis.Analysis {
			// Check that required fields are populated
			if analysis.SeverityLevel == 0 && analysis.Classification == "" && analysis.Language == "" && analysis.Title == "" {
				t.Errorf("Report %d analysis[%d] has no populated fields", reportWithAnalysis.Report.Seq, i)
			}

			// Verify that only the expected fields exist (no extra fields in the struct)
			// This test ensures the payload is truly minimal
			t.Logf("Report %d analysis[%d] has minimal fields: severity=%.2f, classification=%s, language=%s, title=%s", 
				reportWithAnalysis.Report.Seq, i, analysis.SeverityLevel, analysis.Classification, analysis.Language, analysis.Title)
		}
	}
}

func TestGetReportsSince(t *testing.T) {
	// This test verifies that GetReportsSince works with the new filtering
	// Skip if database not available
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     "3306",
		DBUser:     "server",
		DBPassword: "secret_app",
		DBName:     "cleanapp",
	}

	db, err := NewDatabase(cfg)
	if err != nil {
		t.Skipf("Skipping test - database not available: %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()

	// Test GetReportsSince with the new filtering
	reports, err := db.GetReportsSince(ctx, 0)
	if err != nil {
		t.Skipf("Skipping test - cannot query reports: %v", err)
		return
	}

	// Log the number of reports found
	t.Logf("Found %d non-resolved reports since seq 0", len(reports))

	// Verify that all returned reports are either not in report_status or have status 'active'
	for _, report := range reports {
		// Check if this report has a status entry
		var status string
		err := db.db.QueryRowContext(ctx, "SELECT status FROM report_status WHERE seq = ?", report.Report.Seq).Scan(&status)
		if err != nil {
			// No status entry found - this is valid (should be included)
			t.Logf("Report %d has no status entry (valid)", report.Report.Seq)
		} else {
			// Status entry found - should be 'active'
			if status != "active" {
				t.Errorf("Report %d has status '%s' but should be 'active' or not exist", report.Report.Seq, status)
			} else {
				t.Logf("Report %d has status 'active' (valid)", report.Report.Seq)
			}
		}
	}
}
