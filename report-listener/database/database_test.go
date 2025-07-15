package database

import (
	"context"
	"testing"

	"report-listener/config"
)

func TestGetReportsSince(t *testing.T) {
	// This is a basic test to ensure the method signature is correct
	// In a real test environment, you would need a test database with sample data

	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     "3306",
		DBUser:     "test",
		DBPassword: "test",
		DBName:     "test",
	}

	db, err := NewDatabase(cfg)
	if err != nil {
		// Expected to fail without a real database connection
		// This test just ensures the method signature is correct
		t.Skipf("Database connection failed (expected in test environment): %v", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	reports, err := db.GetReportsSince(ctx, 0)
	if err != nil {
		t.Errorf("GetReportsSince failed: %v", err)
		return
	}

	// Verify the structure of returned data
	for _, reportWithAnalysis := range reports {
		// Check that report data is present
		if reportWithAnalysis.Report.Seq <= 0 {
			t.Errorf("Invalid report sequence: %d", reportWithAnalysis.Report.Seq)
		}

		// Check that analysis data is present
		if len(reportWithAnalysis.Analysis) == 0 {
			t.Errorf("No analyses found for report %d", reportWithAnalysis.Report.Seq)
			continue
		}

		// Check each analysis
		for _, analysis := range reportWithAnalysis.Analysis {
			if analysis.Seq != reportWithAnalysis.Report.Seq {
				t.Errorf("Analysis seq (%d) does not match report seq (%d)",
					analysis.Seq, reportWithAnalysis.Report.Seq)
			}

			if analysis.Source == "" {
				t.Errorf("Analysis source is empty for report %d", reportWithAnalysis.Report.Seq)
			}
		}
	}

	t.Logf("Successfully retrieved %d reports with analysis", len(reports))
}
