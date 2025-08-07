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
			name: "valid JSON response - physical",
			response: `{
				"title": "Discarded Mattress and Debris on Roadside",
				"description": "The image shows a rural roadside area with various pieces of litter, including a mattress, wooden debris, and other scattered trash items. This clutter not only detracts from the visual appeal of the environment but also poses a potential safety hazard to passersby, as the debris may obstruct the pathway or create tripping hazards.",
				"classification": "physical",
				"user_info": {
					"name": "John Doe",
					"email": "john@example.com",
					"company": "CleanApp Inc",
					"role": "Field Inspector",
					"company_size": "11-50"
				},
				"location": "123 Main St, Anytown, USA",
				"brand_name": "Generic Mattress Co.",
				"responsible_party": "Generic Mattress Co. Waste Management Team",
				"inferred_contact_emails": ["waste@genericmattress.com", "support@genericmattress.com", "info@genericmattress.com"],
				"suggested_remediation": [
					"Contact local waste management for mattress pickup",
					"Install signage to prevent illegal dumping",
					"Schedule regular cleanup patrols",
					"Coordinate with Generic Mattress Co. for proper disposal"
				],
				"litter_probability": 1.0,
				"hazard_probability": 0.7,
				"digital_bug_probabilty": 0.0,
				"severity_level": 0.6
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:          "Discarded Mattress and Debris on Roadside",
				Description:    "The image shows a rural roadside area with various pieces of litter, including a mattress, wooden debris, and other scattered trash items. This clutter not only detracts from the visual appeal of the environment but also poses a potential safety hazard to passersby, as the debris may obstruct the pathway or create tripping hazards.",
				Classification: "physical",
				UserInfo: UserInfo{
					Name:        "John Doe",
					Email:       "john@example.com",
					Company:     "CleanApp Inc",
					Role:        "Field Inspector",
					CompanySize: "11-50",
				},
				Location:              "123 Main St, Anytown, USA",
				BrandName:             "Generic Mattress Co.",
				ResponsibleParty:      "Generic Mattress Co. Waste Management Team",
				InferredContactEmails: []string{"waste@genericmattress.com", "support@genericmattress.com", "info@genericmattress.com"},
				SuggestedRemediation:  []string{"Contact local waste management for mattress pickup", "Install signage to prevent illegal dumping", "Schedule regular cleanup patrols", "Coordinate with Generic Mattress Co. for proper disposal"},
				LitterProbability:     1.0,
				HazardProbability:     0.7,
				DigitalBugProbability: 0.0,
				SeverityLevel:         0.6,
			},
		},
		{
			name: "valid JSON response - digital",
			response: `{
				"title": "Broken Form Submission on Grok for Business",
				"description": "The early-access request form returns a generic submission error after 3 mandatory fields are completed, blocking lead capture.",
				"classification": "digital",
				"user_info": {
					"name": "Boris Mamlyuk",
					"email": "b@stxn.io",
					"company": "Smart Transactions Corp.",
					"role": "CEO",
					"company_size": "11-50"
				},
				"location": "x.ai/grok",
				"brand_name": "x.ai",
				"responsible_party": "x.ai Web Growth Engineering",
				"inferred_contact_emails": ["support@x.ai", "web@x.ai", "info@x.ai"],
				"suggested_remediation": [
					"Reproduce the POST in dev tools and capture server response",
					"Examine backend logs for 4xx/5xx anomalies linked to the endpoint",
					"Add field-level validation to replace the generic banner",
					"Notify sign-ups once fixed and consider an alternate email form"
				],
				"litter_probability": 0.0,
				"hazard_probability": 0.0,
				"digital_bug_probabilty": 0.9,
				"severity_level": 0.8
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:          "Broken Form Submission on Grok for Business",
				Description:    "The early-access request form returns a generic submission error after 3 mandatory fields are completed, blocking lead capture.",
				Classification: "digital",
				UserInfo: UserInfo{
					Name:        "Boris Mamlyuk",
					Email:       "b@stxn.io",
					Company:     "Smart Transactions Corp.",
					Role:        "CEO",
					CompanySize: "11-50",
				},
				Location:              "x.ai/grok",
				BrandName:             "x.ai",
				ResponsibleParty:      "x.ai Web Growth Engineering",
				InferredContactEmails: []string{"support@x.ai", "web@x.ai", "info@x.ai"},
				SuggestedRemediation:  []string{"Reproduce the POST in dev tools and capture server response", "Examine backend logs for 4xx/5xx anomalies linked to the endpoint", "Add field-level validation to replace the generic banner", "Notify sign-ups once fixed and consider an alternate email form"},
				LitterProbability:     0.0,
				HazardProbability:     0.0,
				DigitalBugProbability: 0.9,
				SeverityLevel:         0.8,
			},
		},
		{
			name: "valid JSON with null user info",
			response: `{
				"title": "Minor Litter Found",
				"description": "Small amount of paper waste visible in the area.",
				"classification": "physical",
				"user_info": {
					"name": null,
					"email": null,
					"company": null,
					"role": null,
					"company_size": null
				},
				"location": "Central Park, New York",
				"brand_name": "Coca-Cola",
				"responsible_party": "Coca-Cola Sustainability Team",
				"inferred_contact_emails": ["sustainability@coca-cola.com", "support@coca-cola.com"],
				"suggested_remediation": [
					"Install additional trash bins",
					"Increase cleaning frequency",
					"Add recycling bins",
					"Launch awareness campaign"
				],
				"litter_probability": 0.3,
				"hazard_probability": 0.1,
				"digital_bug_probabilty": 0.0,
				"severity_level": 0.2
			}`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:          "Minor Litter Found",
				Description:    "Small amount of paper waste visible in the area.",
				Classification: "physical",
				UserInfo: UserInfo{
					Name:        "",
					Email:       "",
					Company:     "",
					Role:        "",
					CompanySize: "",
				},
				Location:              "Central Park, New York",
				BrandName:             "Coca-Cola",
				ResponsibleParty:      "Coca-Cola Sustainability Team",
				InferredContactEmails: []string{"sustainability@coca-cola.com", "support@coca-cola.com"},
				SuggestedRemediation:  []string{"Install additional trash bins", "Increase cleaning frequency", "Add recycling bins", "Launch awareness campaign"},
				LitterProbability:     0.3,
				HazardProbability:     0.1,
				DigitalBugProbability: 0.0,
				SeverityLevel:         0.2,
			},
		},
		{
			name: "invalid classification",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"classification": "invalid",
				"user_info": {
					"name": "Test User",
					"email": "test@example.com",
					"company": "Test Corp",
					"role": "Tester",
					"company_size": "1-10"
				},
				"location": "Test Location",
				"brand_name": "Test Brand",
				"responsible_party": "Test Team",
				"inferred_contact_emails": ["test@example.com"],
				"suggested_remediation": ["Test step 1", "Test step 2"],
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"digital_bug_probabilty": 0.1,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "missing classification",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"user_info": {
					"name": "Test User",
					"email": "test@example.com",
					"company": "Test Corp",
					"role": "Tester",
					"company_size": "1-10"
				},
				"location": "Test Location",
				"brand_name": "Test Brand",
				"responsible_party": "Test Team",
				"inferred_contact_emails": ["test@example.com"],
				"suggested_remediation": ["Test step 1", "Test step 2"],
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"digital_bug_probabilty": 0.1,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "invalid digital bug probability",
			response: `{
				"title": "Some Title",
				"description": "Some description",
				"classification": "digital",
				"user_info": {
					"name": "Test User",
					"email": "test@example.com",
					"company": "Test Corp",
					"role": "Tester",
					"company_size": "1-10"
				},
				"location": "Test Location",
				"brand_name": "Test Brand",
				"responsible_party": "Test Team",
				"inferred_contact_emails": ["test@example.com"],
				"suggested_remediation": ["Test step 1", "Test step 2"],
				"litter_probability": 0.5,
				"hazard_probability": 0.3,
				"digital_bug_probabilty": 1.5,
				"severity_level": 0.4
			}`,
			wantErr:  true,
			expected: nil,
		},
		{
			name: "markdown formatted JSON",
			response: `Here is the analysis:

` + "```" + `json
{
  "title": "Ad Analytics Gender Breakdown Exceeds 100%",
  "description": "The insights widget displays 0% men and 101.6% women, so demographics total 101.6%. This indicates a percentage-calculation defect in the Meta Ads analytics pipeline.",
  "classification": "digital",
  "user_info": {
    "name": null,
    "email": null,
    "company": null,
    "role": null,
    "company_size": null
  },
  "location": "Meta / Instagram Ads Insights UI",
  "brand_name": "Meta",
  "responsible_party": "Meta Ads Insights Engineering & Data QA Team",
  "inferred_contact_emails": ["ads-support@meta.com", "analytics-qa@meta.com", "support@fb.com"],
  "suggested_remediation": [
    "Audit the aggregation query to ensure gender percentages are normalised to 100%",
    "Verify rounding rules and apply compensating adjustments before display",
    "Ship a unit test that fails if demographic sums deviate from 100 ± 0.1%",
    "Back-fill historical insight records and email affected advertisers once corrected"
  ],
  "litter_probability": 0.0,
  "hazard_probability": 0.0,
  "digital_bug_probabilty": 0.95,
  "severity_level": 0.7
}
` + "```" + `

This analysis shows a digital bug.`,
			wantErr: false,
			expected: &AnalysisResult{
				Title:          "Ad Analytics Gender Breakdown Exceeds 100%",
				Description:    "The insights widget displays 0% men and 101.6% women, so demographics total 101.6%. This indicates a percentage-calculation defect in the Meta Ads analytics pipeline.",
				Classification: "digital",
				UserInfo: UserInfo{
					Name:        "",
					Email:       "",
					Company:     "",
					Role:        "",
					CompanySize: "",
				},
				Location:              "Meta / Instagram Ads Insights UI",
				BrandName:             "Meta",
				ResponsibleParty:      "Meta Ads Insights Engineering & Data QA Team",
				InferredContactEmails: []string{"ads-support@meta.com", "analytics-qa@meta.com", "support@fb.com"},
				SuggestedRemediation:  []string{"Audit the aggregation query to ensure gender percentages are normalised to 100%", "Verify rounding rules and apply compensating adjustments before display", "Ship a unit test that fails if demographic sums deviate from 100 ± 0.1%", "Back-fill historical insight records and email affected advertisers once corrected"},
				LitterProbability:     0.0,
				HazardProbability:     0.0,
				DigitalBugProbability: 0.95,
				SeverityLevel:         0.7,
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

			// Test all fields
			if result.Title != tt.expected.Title {
				t.Errorf("ParseAnalysis() title = %v, want %v", result.Title, tt.expected.Title)
			}

			if result.Description != tt.expected.Description {
				t.Errorf("ParseAnalysis() description = %v, want %v", result.Description, tt.expected.Description)
			}

			if result.Classification != tt.expected.Classification {
				t.Errorf("ParseAnalysis() classification = %v, want %v", result.Classification, tt.expected.Classification)
			}

			// Test UserInfo fields
			if result.UserInfo.Name != tt.expected.UserInfo.Name {
				t.Errorf("ParseAnalysis() user_info.name = %v, want %v", result.UserInfo.Name, tt.expected.UserInfo.Name)
			}

			if result.UserInfo.Email != tt.expected.UserInfo.Email {
				t.Errorf("ParseAnalysis() user_info.email = %v, want %v", result.UserInfo.Email, tt.expected.UserInfo.Email)
			}

			if result.UserInfo.Company != tt.expected.UserInfo.Company {
				t.Errorf("ParseAnalysis() user_info.company = %v, want %v", result.UserInfo.Company, tt.expected.UserInfo.Company)
			}

			if result.UserInfo.Role != tt.expected.UserInfo.Role {
				t.Errorf("ParseAnalysis() user_info.role = %v, want %v", result.UserInfo.Role, tt.expected.UserInfo.Role)
			}

			if result.UserInfo.CompanySize != tt.expected.UserInfo.CompanySize {
				t.Errorf("ParseAnalysis() user_info.company_size = %v, want %v", result.UserInfo.CompanySize, tt.expected.UserInfo.CompanySize)
			}

			if result.Location != tt.expected.Location {
				t.Errorf("ParseAnalysis() location = %v, want %v", result.Location, tt.expected.Location)
			}

			if result.BrandName != tt.expected.BrandName {
				t.Errorf("ParseAnalysis() brand_name = %v, want %v", result.BrandName, tt.expected.BrandName)
			}

			if result.ResponsibleParty != tt.expected.ResponsibleParty {
				t.Errorf("ParseAnalysis() responsible_party = %v, want %v", result.ResponsibleParty, tt.expected.ResponsibleParty)
			}

			// Test arrays
			if len(result.InferredContactEmails) != len(tt.expected.InferredContactEmails) {
				t.Errorf("ParseAnalysis() inferred_contact_emails length = %v, want %v", len(result.InferredContactEmails), len(tt.expected.InferredContactEmails))
			} else {
				for i, email := range result.InferredContactEmails {
					if email != tt.expected.InferredContactEmails[i] {
						t.Errorf("ParseAnalysis() inferred_contact_emails[%d] = %v, want %v", i, email, tt.expected.InferredContactEmails[i])
					}
				}
			}

			if len(result.SuggestedRemediation) != len(tt.expected.SuggestedRemediation) {
				t.Errorf("ParseAnalysis() suggested_remediation length = %v, want %v", len(result.SuggestedRemediation), len(tt.expected.SuggestedRemediation))
			} else {
				for i, step := range result.SuggestedRemediation {
					if step != tt.expected.SuggestedRemediation[i] {
						t.Errorf("ParseAnalysis() suggested_remediation[%d] = %v, want %v", i, step, tt.expected.SuggestedRemediation[i])
					}
				}
			}

			if result.LitterProbability != tt.expected.LitterProbability {
				t.Errorf("ParseAnalysis() litter_probability = %v, want %v", result.LitterProbability, tt.expected.LitterProbability)
			}

			if result.HazardProbability != tt.expected.HazardProbability {
				t.Errorf("ParseAnalysis() hazard_probability = %v, want %v", result.HazardProbability, tt.expected.HazardProbability)
			}

			if result.DigitalBugProbability != tt.expected.DigitalBugProbability {
				t.Errorf("ParseAnalysis() digital_bug_probabilty = %v, want %v", result.DigitalBugProbability, tt.expected.DigitalBugProbability)
			}

			if result.SeverityLevel != tt.expected.SeverityLevel {
				t.Errorf("ParseAnalysis() severity_level = %v, want %v", result.SeverityLevel, tt.expected.SeverityLevel)
			}
		})
	}
}
