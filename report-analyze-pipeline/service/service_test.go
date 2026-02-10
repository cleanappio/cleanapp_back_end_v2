package service

import (
	"testing"

	"report-analyze-pipeline/database"
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
