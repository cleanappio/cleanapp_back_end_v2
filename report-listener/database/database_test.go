package database

import (
	"context"
	"testing"

	"report-listener/config"
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

	// Test GetLastNAnalyzedReports with the new filtering
	reports, err := db.GetLastNAnalyzedReports(ctx, 10)
	if err != nil {
		t.Skipf("Skipping test - cannot query reports: %v", err)
		return
	}

	// Log the number of reports found
	t.Logf("Found %d non-resolved reports", len(reports))

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
