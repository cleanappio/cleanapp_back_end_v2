package services

import (
	"testing"
)

func TestBrandService_NormalizeBrandName(t *testing.T) {
	brandService := NewBrandService()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "coca-cola normalization",
			input:    "Coca-Cola",
			expected: "cocacola",
		},
		{
			name:     "coca cola normalization",
			input:    "Coca Cola",
			expected: "cocacola",
		},
		{
			name:     "red bull normalization",
			input:    "Red Bull",
			expected: "redbull",
		},
		{
			name:     "nike with punctuation",
			input:    "Nike, Inc.",
			expected: "nikeinc",
		},
		{
			name:     "adidas with underscore",
			input:    "Adidas_Group",
			expected: "adidasgroup",
		},
		{
			name:     "brand with and",
			input:    "Coca and Cola",
			expected: "cocacola",
		},
		{
			name:     "brand with ampersand",
			input:    "Coca & Cola",
			expected: "cocacola",
		},
		{
			name:     "mcdonalds with apostrophe",
			input:    "McDonald's",
			expected: "mcdonalds",
		},
		{
			name:     "starbucks coffee",
			input:    "Starbucks Coffee",
			expected: "starbuckscoffee",
		},
		{
			name:     "empty brand name",
			input:    "",
			expected: "",
		},
		{
			name:     "null brand name",
			input:    "null",
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := brandService.NormalizeBrandName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeBrandName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBrandService_GetBrandDisplayName(t *testing.T) {
	brandService := NewBrandService()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "coca-cola display name",
			input:    "coca-cola",
			expected: "Coca-Cola",
		},
		{
			name:     "redbull display name",
			input:    "redbull",
			expected: "Red Bull",
		},
		{
			name:     "nike display name",
			input:    "nike",
			expected: "Nike",
		},
		{
			name:     "adidas display name",
			input:    "adidas",
			expected: "Adidas",
		},
		{
			name:     "pepsi display name",
			input:    "pepsi",
			expected: "Pepsi",
		},
		{
			name:     "mcdonalds display name",
			input:    "mcdonalds",
			expected: "McDonald's",
		},
		{
			name:     "starbucks display name",
			input:    "starbucks",
			expected: "Starbucks",
		},
		{
			name:     "apple display name",
			input:    "apple",
			expected: "Apple",
		},
		{
			name:     "samsung display name",
			input:    "samsung",
			expected: "Samsung",
		},
		{
			name:     "microsoft display name",
			input:    "microsoft",
			expected: "Microsoft",
		},
		{
			name:     "unknown brand title case",
			input:    "unknown brand",
			expected: "Unknown Brand",
		},
		{
			name:     "empty brand name",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := brandService.GetBrandDisplayName(tt.input)
			if result != tt.expected {
				t.Errorf("GetBrandDisplayName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBrandService_ResolveBrandPreservesSubjectAndCanonicalizesExactAliases(t *testing.T) {
	brandService := NewBrandService()

	tests := []struct {
		name               string
		rawBrand           string
		context            []string
		expectedNormalized string
		expectedDisplay    string
	}{
		{
			name:               "selected vendor survives product mentions",
			rawBrand:           "Anthropic",
			context:            []string{"Major Claude outage and credit bug in Claude web app"},
			expectedNormalized: "anthropic",
			expectedDisplay:    "Anthropic",
		},
		{
			name:               "selected Google survives NotebookLM mention",
			rawBrand:           "Google",
			context:            []string{"NotebookLM captions are failing and narrations are blank"},
			expectedNormalized: "google",
			expectedDisplay:    "Google",
		},
		{
			name:               "selected Meta survives Instagram mention",
			rawBrand:           "Meta",
			context:            []string{"Instagram share button does not work on iOS"},
			expectedNormalized: "meta",
			expectedDisplay:    "Meta",
		},
		{
			name:               "plain vendor remains vendor without product context",
			rawBrand:           "Anthropic",
			context:            []string{"General vendor outage"},
			expectedNormalized: "anthropic",
			expectedDisplay:    "Anthropic",
		},
		{
			name:               "explicit complaint subject survives vendor and product examples",
			rawBrand:           "CleanApp",
			context:            []string{"On CleanApp itself, I can't easily cluster multiple properties or sites into one brand cluster, eg clustering Grok and Grokipedia and XAI and X and SpaceX"},
			expectedNormalized: "cleanapp",
			expectedDisplay:    "CleanApp",
		},
		{
			name:               "attached screenshot context cannot replace selected subject",
			rawBrand:           "CleanApp",
			context:            []string{"problem with CleanApp brand attribution logic", "Screenshot: Grok report; examples Grok and XAI"},
			expectedNormalized: "cleanapp",
			expectedDisplay:    "CleanApp",
		},
		{
			name:               "unknown subject is not filled from examples",
			rawBrand:           "",
			context:            []string{"Can't easily cluster multiple properties into one brand cluster, eg Grok and Grokipedia and XAI and X and SpaceX"},
			expectedNormalized: "",
			expectedDisplay:    "",
		},
		{
			name:               "literal null subject remains unknown",
			rawBrand:           "null",
			context:            []string{"Grok and xAI were examples"},
			expectedNormalized: "",
			expectedDisplay:    "",
		},
		{
			name:               "another explicit product is not replaced by a prominent AI product",
			rawBrand:           "Stripe",
			context:            []string{"Stripe fails to bill our OpenAI ChatGPT integration"},
			expectedNormalized: "stripe",
			expectedDisplay:    "Stripe",
		},
		{
			name:               "explicit product does not depend on alias rule order",
			rawBrand:           "ChatGPT",
			context:            []string{"ChatGPT fails but Anthropic Claude works"},
			expectedNormalized: "chatgpt",
			expectedDisplay:    "ChatGPT",
		},
		{
			name:               "multiple products cannot narrow a vendor by table order",
			rawBrand:           "Google",
			context:            []string{"Google billing issue for Gemini and NotebookLM"},
			expectedNormalized: "google",
			expectedDisplay:    "Google",
		},
		{
			name:               "a product name inside another brand is not a match",
			rawBrand:           "xAI",
			context:            []string{"Grokipedia has an incorrect citation"},
			expectedNormalized: "xai",
			expectedDisplay:    "Xai",
		},
		{
			name:               "a selected distinct product is not a substring alias",
			rawBrand:           "Grokipedia",
			context:            []string{"Grokipedia has an incorrect citation"},
			expectedNormalized: "grokipedia",
			expectedDisplay:    "Grokipedia",
		},
		{
			name:               "vendor names are matched exactly",
			rawBrand:           "Google Workspace",
			context:            []string{"Google Workspace cannot open a Gemini export"},
			expectedNormalized: "googleworkspace",
			expectedDisplay:    "Google Workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, display := brandService.ResolveBrand(tt.rawBrand, tt.context...)
			if normalized != tt.expectedNormalized {
				t.Fatalf("ResolveBrand() normalized = %q, want %q", normalized, tt.expectedNormalized)
			}
			if display != tt.expectedDisplay {
				t.Fatalf("ResolveBrand() display = %q, want %q", display, tt.expectedDisplay)
			}
		})
	}
}
