package services

import (
	"log"
	"strings"
	"unicode"
)

// BrandService manages brand name normalization
type BrandService struct{}

// NewBrandService creates a new brand service
func NewBrandService() *BrandService {
	return &BrandService{}
}

// NormalizeBrandName normalizes a brand name for consistent storage
// This function handles common variations and standardizes brand names
func (s *BrandService) NormalizeBrandName(brandName string) string {
	if brandName == "" {
		return ""
	}

	// Convert to lowercase
	normalized := strings.ToLower(brandName)

	// Remove common punctuation and spaces
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.ReplaceAll(normalized, "&", "")
	normalized = strings.ReplaceAll(normalized, "'", "")
	// Don't remove "and" as it's part of many brand names
	// normalized = strings.ReplaceAll(normalized, "and", "")

	// Remove extra spaces
	normalized = strings.Join(strings.Fields(normalized), "")

	log.Printf("Normalizing brand name: %s -> %s", brandName, normalized)

	return normalized
}

// toTitleCase converts a string to title case
func (s *BrandService) toTitleCase(str string) string {
	if str == "" {
		return str
	}

	runes := []rune(str)
	runes[0] = unicode.ToUpper(runes[0])

	for i := 1; i < len(runes); i++ {
		if unicode.IsSpace(runes[i-1]) || runes[i-1] == '-' || runes[i-1] == '_' {
			runes[i] = unicode.ToUpper(runes[i])
		} else {
			runes[i] = unicode.ToLower(runes[i])
		}
	}

	return string(runes)
}
