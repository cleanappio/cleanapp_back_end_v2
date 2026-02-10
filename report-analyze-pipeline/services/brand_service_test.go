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
