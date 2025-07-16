package services

import (
	"testing"
)

func TestBrandService_IsBrandMatch(t *testing.T) {
	brandNames := []string{"coca-cola", "redbull", "nike", "adidas"}
	brandService := NewBrandService(brandNames)

	tests := []struct {
		name        string
		input       string
		expectMatch bool
		expectBrand string
	}{
		{
			name:        "exact match coca-cola",
			input:       "coca-cola",
			expectMatch: true,
			expectBrand: "coca-cola",
		},
		{
			name:        "soft match coca cola",
			input:       "coca cola",
			expectMatch: true,
			expectBrand: "coca-cola",
		},
		{
			name:        "soft match Coca Cola",
			input:       "Coca Cola",
			expectMatch: true,
			expectBrand: "coca-cola",
		},
		{
			name:        "soft match COCA COLA",
			input:       "COCA COLA",
			expectMatch: true,
			expectBrand: "coca-cola",
		},
		{
			name:        "exact match redbull",
			input:       "redbull",
			expectMatch: true,
			expectBrand: "redbull",
		},
		{
			name:        "soft match Red Bull",
			input:       "Red Bull",
			expectMatch: true,
			expectBrand: "redbull",
		},
		{
			name:        "soft match red bull",
			input:       "red bull",
			expectMatch: true,
			expectBrand: "redbull",
		},
		{
			name:        "exact match nike",
			input:       "nike",
			expectMatch: true,
			expectBrand: "nike",
		},
		{
			name:        "soft match NIKE",
			input:       "NIKE",
			expectMatch: true,
			expectBrand: "nike",
		},
		{
			name:        "soft match Nike Shoes",
			input:       "Nike Shoes",
			expectMatch: true,
			expectBrand: "nike",
		},
		{
			name:        "exact match adidas",
			input:       "adidas",
			expectMatch: true,
			expectBrand: "adidas",
		},
		{
			name:        "soft match Adidas",
			input:       "Adidas",
			expectMatch: true,
			expectBrand: "adidas",
		},
		{
			name:        "no match unknown brand",
			input:       "unknown brand",
			expectMatch: false,
			expectBrand: "",
		},
		{
			name:        "empty input",
			input:       "",
			expectMatch: false,
			expectBrand: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isMatch, matchedBrand := brandService.IsBrandMatch(tt.input)

			if isMatch != tt.expectMatch {
				t.Errorf("IsBrandMatch() match = %v, want %v", isMatch, tt.expectMatch)
			}

			if matchedBrand != tt.expectBrand {
				t.Errorf("IsBrandMatch() brand = %v, want %v", matchedBrand, tt.expectBrand)
			}
		})
	}
}

func TestBrandService_GetBrandDisplayName(t *testing.T) {
	brandService := NewBrandService([]string{})

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
			name:     "empty input",
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

func TestBrandService_GetBrandNames(t *testing.T) {
	brandNames := []string{"coca-cola", "redbull", "nike", "adidas"}
	brandService := NewBrandService(brandNames)

	result := brandService.GetBrandNames()

	if len(result) != len(brandNames) {
		t.Errorf("GetBrandNames() returned %d brands, want %d", len(result), len(brandNames))
	}

	for i, brand := range result {
		if brand != brandNames[i] {
			t.Errorf("GetBrandNames()[%d] = %v, want %v", i, brand, brandNames[i])
		}
	}
}
