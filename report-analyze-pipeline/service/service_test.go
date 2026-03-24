package service

import (
	"strings"
	"testing"

	"report-analyze-pipeline/database"
	"report-analyze-pipeline/parser"
	"report-analyze-pipeline/services"
)

func TestBrandDisplayNamePopulation(t *testing.T) {
	// Create a brand service
	brandService := services.NewBrandService()

	// Test cases for brand name normalization and display name generation
	testCases := []struct {
		inputBrandName     string
		expectedNormalized string
		expectedDisplay    string
	}{
		{"Coca-Cola", "cocacola", "Coca-Cola"},
		{"coca-cola", "cocacola", "Coca-Cola"},
		{"COCA COLA", "cocacola", "Coca Cola"},
		{"Red Bull", "redbull", "Red Bull"},
		{"red bull", "redbull", "Red Bull"},
		{"Nike", "nike", "Nike"},
		{"nike", "nike", "Nike"},
		{"McDonald's", "mcdonalds", "McDonald's"},
		{"mcdonalds", "mcdonalds", "McDonald's"},
		{"", "", ""},
		{"Some Random Brand", "somerandombrand", "Some Random Brand"},
	}

	for _, tc := range testCases {
		t.Run(tc.inputBrandName, func(t *testing.T) {
			// Test normalization
			normalized := brandService.NormalizeBrandName(tc.inputBrandName)
			if normalized != tc.expectedNormalized {
				t.Errorf("NormalizeBrandName(%q) = %q, want %q", tc.inputBrandName, normalized, tc.expectedNormalized)
			}

			// Test display name
			displayName := brandService.GetBrandDisplayName(tc.inputBrandName)
			if displayName != tc.expectedDisplay {
				t.Errorf("GetBrandDisplayName(%q) = %q, want %q", tc.inputBrandName, displayName, tc.expectedDisplay)
			}
		})
	}
}

func TestReportAnalysisWithBrandDisplayName(t *testing.T) {
	// Create a mock analysis result
	analysis := &database.ReportAnalysis{
		Seq:                   1,
		Source:                "test",
		AnalysisText:          "Test analysis",
		Title:                 "Test Title",
		Description:           "Test Description",
		BrandName:             "cocacola",
		BrandDisplayName:      "Coca-Cola",
		LitterProbability:     0.5,
		HazardProbability:     0.3,
		SeverityLevel:         0.4,
		Summary:               "Test Summary",
		Language:              "en",
		Classification:        "physical",
		InferredContactEmails: "test@example.com, contact@brand.com",
	}

	// Verify the fields are set correctly
	if analysis.BrandName != "cocacola" {
		t.Errorf("Expected BrandName to be 'cocacola', got %q", analysis.BrandName)
	}

	if analysis.BrandDisplayName != "Coca-Cola" {
		t.Errorf("Expected BrandDisplayName to be 'Coca-Cola', got %q", analysis.BrandDisplayName)
	}

	if analysis.Classification != "physical" {
		t.Errorf("Expected Classification to be 'physical', got %q", analysis.Classification)
	}

	if analysis.InferredContactEmails != "test@example.com, contact@brand.com" {
		t.Errorf("Expected InferredContactEmails to be 'test@example.com, contact@brand.com', got %q", analysis.InferredContactEmails)
	}
}

func TestNormalizeClassificationTreatsRealWorldHazardAsPhysical(t *testing.T) {
	report := &database.Report{
		Seq:         1182368,
		Description: "Satellite telemetry detected a large fire at the Riyadh refinery after a likely pipeline breach.",
	}
	analysis := &parser.AnalysisResult{
		Title:                 "Catastrophic Fire and Pipeline Breach at Riyadh Refinery",
		Description:           "A real-world refinery fire and pipeline incident is visible in the monitored area.",
		Classification:        parser.ClassificationDigital,
		HazardProbability:     1.0,
		DigitalBugProbability: 0.1,
		SeverityLevel:         1.0,
		BrandName:             "x(twitter)",
	}

	got := normalizeClassification(report, analysis)
	if got != parser.ClassificationPhysical {
		t.Fatalf("normalizeClassification(...) = %q, want %q", got, parser.ClassificationPhysical)
	}
}

func TestNormalizeClassificationKeepsSoftwareBugDigital(t *testing.T) {
	report := &database.Report{
		Seq:         1,
		Description: "Users cannot submit the checkout form in the mobile app.",
	}
	analysis := &parser.AnalysisResult{
		Title:                 "Checkout Submission Failure in Mobile App",
		Description:           "The app throws an error message on form submission and never completes checkout.",
		Classification:        parser.ClassificationDigital,
		HazardProbability:     0.05,
		DigitalBugProbability: 0.92,
		SeverityLevel:         0.72,
		BrandName:             "CleanApp",
	}

	got := normalizeClassification(report, analysis)
	if got != parser.ClassificationDigital {
		t.Fatalf("normalizeClassification(...) = %q, want %q", got, parser.ClassificationDigital)
	}
}

func TestBuildAnalysisInputPrefersShareContextOverGenericHumanDescription(t *testing.T) {
	report := &database.Report{
		Seq:         1182381,
		Description: "Human report submission",
		SharedText:  "Claude is down and credit usage is incorrect on the iOS app.",
		SourceURL:   "https://x.com/example/status/123",
		SourceApp:   "x",
	}

	got := buildAnalysisInput(report)
	if got == "" {
		t.Fatal("buildAnalysisInput() returned empty string")
	}
	if got == report.Description {
		t.Fatalf("buildAnalysisInput() = %q, want enriched share context instead of generic description", got)
	}
	for _, expected := range []string{"Claude is down", "https://x.com/example/status/123", "Source app: x"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("buildAnalysisInput() missing %q in %q", expected, got)
		}
	}
}
