package parser

import (
	"testing"
)

func TestParseAnalysis(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  bool
		expected *AnalysisResult
	}{
		{
			name: "valid JSON response",
			response: `{
				"title": "Discarded Mattress and Debris on Roadside",
				"description": "The image shows a rural roadside area with various pieces of litter, including a mattress, wooden debris, and other scattered trash items. This clutter not only detracts from the visual appeal of the environment but also poses a potential safety hazard to passersby, as the debris may obstruct the pathway or create tripping hazards.",
				"brand_name": "Generic Mattress Co.",
				"litter_probability": 1.0,
				"hazard_probability": 0.7,
				"severity_level": 0.6
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Discarded Mattress and Debris on Roadside",
				Description:       "The image shows a rural roadside area with various pieces of litter, including a mattress, wooden debris, and other scattered trash items. This clutter not only detracts from the visual appeal of the environment but also poses a potential safety hazard to passersby, as the debris may obstruct the pathway or create tripping hazards.",
				BrandName:         "Generic Mattress Co.",
				LitterProbability: 1.0,
				HazardProbability: 0.7,
				SeverityLevel:     0.6,
			},
		},
		{
			name: "valid JSON with decimal values",
			response: `{
				"title": "Minor Litter Found",
				"description": "Small amount of paper waste visible in the area.",
				"brand_name": "Coca-Cola",
				"litter_probability": 0.3,
				"hazard_probability": 0.1,
				"severity_level": 0.2
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Minor Litter Found",
				Description:       "Small amount of paper waste visible in the area.",
				BrandName:         "Coca-Cola",
				LitterProbability: 0.3,
				HazardProbability: 0.1,
				SeverityLevel:     0.2,
			},
		},
		{
			name: "valid JSON with empty brand name",
			response: `{
				"title": "Unknown Brand Litter",
				"description": "Various unidentified brand items found in the area.",
				"brand_name": null,
				"litter_probability": 0.5,
				"hazard_probability": 0.2,
				"severity_level": 0.4
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Unknown Brand Litter",
				Description:       "Various unidentified brand items found in the area.",
				BrandName:         "",
				LitterProbability: 0.5,
				HazardProbability: 0.2,
				SeverityLevel:     0.4,
			},
		},
		{
			name: "valid JSON without brand name field",
			response: `{
				"title": "Natural Debris",
				"description": "Natural debris and fallen branches in the area.",
				"litter_probability": 0.1,
				"hazard_probability": 0.3,
				"severity_level": 0.2
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Natural Debris",
				Description:       "Natural debris and fallen branches in the area.",
				BrandName:         "",
				LitterProbability: 0.1,
				HazardProbability: 0.3,
				SeverityLevel:     0.2,
			},
		},
		{
			name: "valid JSON with multiple word brand name",
			response: `{
				"title": "Fast Food Packaging Litter",
				"description": "Multiple fast food containers and wrappers scattered in the area.",
				"brand_name": "McDonald's Corporation",
				"litter_probability": 0.8,
				"hazard_probability": 0.1,
				"severity_level": 0.5
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Fast Food Packaging Litter",
				Description:       "Multiple fast food containers and wrappers scattered in the area.",
				BrandName:         "McDonald's Corporation",
				LitterProbability: 0.8,
				HazardProbability: 0.1,
				SeverityLevel:     0.5,
			},
		},
		{
			name:     "invalid JSON",
			response: `{"title": "Invalid JSON`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "missing title",
			response: `{
				"description": "Some description",
				"brand_name": "Some Brand",
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "missing description",
			response: `{
				"title": "Some Title",
				"brand_name": "Some Brand",
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "invalid litter probability",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"brand_name": "Some Brand",
				"litter_probability": 1.5,
				"hazard_probability": 0.3,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "invalid hazard probability",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"brand_name": "Some Brand",
				"hazard_probability": -0.1,
				"litter_probability": 0.5,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "invalid severity level",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"brand_name": "Some Brand",
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"severity_level": 2.0
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "markdown formatted JSON",
			response: `Here is the analysis:

` + "```" + `json
{
  "title": "Wooden Wall with Varied Plank Colors",
  "description": "The image shows a close-up of a wall or floor made of wooden planks. The planks have varying shades of brown, green, and gray, giving the surface a patchwork or reclaimed wood appearance. There are no visible objects, trash, or hazards present in the image.",
  "brand_name": "TimberCraft Industries",
  "litter_probability": 0.0,
  "hazard_probability": 0.0,
  "severity_level": 0.0
}
` + "```" + `

This analysis shows no litter or hazards.`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Wooden Wall with Varied Plank Colors",
				Description:       "The image shows a close-up of a wall or floor made of wooden planks. The planks have varying shades of brown, green, and gray, giving the surface a patchwork or reclaimed wood appearance. There are no visible objects, trash, or hazards present in the image.",
				BrandName:         "TimberCraft Industries",
				LitterProbability: 0.0,
				HazardProbability: 0.0,
				SeverityLevel:     0.0,
			},
		},
		{
			name: "markdown formatted JSON without language identifier",
			response: `Analysis result:

` + "```" + `
{
  "title": "Clean Environment",
  "description": "The area appears to be clean with no visible litter or hazards.",
  "brand_name": "",
  "litter_probability": 0.0,
  "hazard_probability": 0.0,
  "severity_level": 0.0
}
` + "```" + ``,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Clean Environment",
				Description:       "The area appears to be clean with no visible litter or hazards.",
				BrandName:         "",
				LitterProbability: 0.0,
				HazardProbability: 0.0,
				SeverityLevel:     0.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAnalysis(tt.response)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAnalysis() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseAnalysis() unexpected error: %v", err)
				return
			}

			if result.Title != tt.expected.Title {
				t.Errorf("ParseAnalysis() title = %v, want %v", result.Title, tt.expected.Title)
			}

			if result.Description != tt.expected.Description {
				t.Errorf("ParseAnalysis() description = %v, want %v", result.Description, tt.expected.Description)
			}

			if result.BrandName != tt.expected.BrandName {
				t.Errorf("ParseAnalysis() brand_name = %v, want %v", result.BrandName, tt.expected.BrandName)
			}

			if result.LitterProbability != tt.expected.LitterProbability {
				t.Errorf("ParseAnalysis() litter_probability = %v, want %v", result.LitterProbability, tt.expected.LitterProbability)
			}

			if result.HazardProbability != tt.expected.HazardProbability {
				t.Errorf("ParseAnalysis() hazard_probability = %v, want %v", result.HazardProbability, tt.expected.HazardProbability)
			}

			if result.SeverityLevel != tt.expected.SeverityLevel {
				t.Errorf("ParseAnalysis() severity_level = %v, want %v", result.SeverityLevel, tt.expected.SeverityLevel)
			}
		})
	}
}
