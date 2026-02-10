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
	// Note: we try to preserve word boundaries while stripping tokens like "and"
	// (e.g. "Coca and Cola" -> "cocacola") before collapsing whitespace.
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.ReplaceAll(normalized, "&", " ")
	normalized = strings.ReplaceAll(normalized, "'", "")
	normalized = strings.ReplaceAll(normalized, "’", "")
	normalized = strings.ReplaceAll(normalized, " and ", " ")

	// Remove extra spaces
	normalized = strings.Join(strings.Fields(normalized), "")

	log.Printf("Normalizing brand name: %s -> %s", brandName, normalized)

	return normalized
}

// GetBrandDisplayName returns a display-friendly brand name.
//
// Input may be raw ("COCA COLA") or normalized ("cocacola").
func (s *BrandService) GetBrandDisplayName(brandName string) string {
	if brandName == "" {
		return ""
	}

	// Normalize for lookup, but keep the original for generic title-casing.
	n := strings.ToLower(strings.TrimSpace(brandName))
	switch n {
	case "cocacola", "coca-cola":
		return "Coca-Cola"
	case "redbull", "red bull":
		return "Red Bull"
	case "mcdonalds", "mcdonald's":
		return "McDonald's"
	}

	// Default: title-case the input as given.
	return s.toTitleCase(brandName)
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
