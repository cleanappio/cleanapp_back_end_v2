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
				"litter_probability": 1.0,
				"hazard_probability": 0.7,
				"severity_level": 0.6
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Discarded Mattress and Debris on Roadside",
				Description:       "The image shows a rural roadside area with various pieces of litter, including a mattress, wooden debris, and other scattered trash items. This clutter not only detracts from the visual appeal of the environment but also poses a potential safety hazard to passersby, as the debris may obstruct the pathway or create tripping hazards.",
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
				"litter_probability": 0.3,
				"hazard_probability": 0.1,
				"severity_level": 0.2
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:             "Minor Litter Found",
				Description:       "Small amount of paper waste visible in the area.",
				LitterProbability: 0.3,
				HazardProbability: 0.1,
				SeverityLevel:     0.2,
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
				"litter_probability": 0.5,
				"hazard_probability": -0.1,
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
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"severity_level": 2.0
			}`,
			wantErr:  true,
			expected: nil,
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
