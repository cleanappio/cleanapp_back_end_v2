package services

import (
	"strings"
	"unicode"
)

// BrandService manages brand name matching and normalization
type BrandService struct {
	brandNames []string
}

// NewBrandService creates a new brand service
func NewBrandService(brandNames []string) *BrandService {
	return &BrandService{
		brandNames: brandNames,
	}
}

// GetBrandNames returns the configured brand names
func (s *BrandService) GetBrandNames() []string {
	return s.brandNames
}

// IsBrandMatch checks if a given brand name matches any of the configured brands
// Uses soft matching to handle variations like "coca cola" -> "coca-cola"
func (s *BrandService) IsBrandMatch(brandName string) (bool, string) {
	if brandName == "" {
		return false, ""
	}

	// Normalize the input brand name
	normalizedInput := s.normalizeBrandName(brandName)

	// Check against each configured brand name
	for _, configuredBrand := range s.brandNames {
		normalizedConfig := s.normalizeBrandName(configuredBrand)

		if s.softMatch(normalizedInput, normalizedConfig) {
			return true, configuredBrand
		}
	}

	return false, ""
}

// normalizeBrandName normalizes a brand name for comparison
func (s *BrandService) normalizeBrandName(brandName string) string {
	// Convert to lowercase
	normalized := strings.ToLower(brandName)

	// Remove common punctuation and spaces
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", "")
	normalized = strings.ReplaceAll(normalized, "&", "")
	normalized = strings.ReplaceAll(normalized, "and", "")

	// Remove extra spaces
	normalized = strings.Join(strings.Fields(normalized), "")

	return normalized
}

// softMatch performs soft matching between two normalized brand names
func (s *BrandService) softMatch(input, target string) bool {
	// Exact match
	if input == target {
		return true
	}

	// Check if input contains target or target contains input
	if strings.Contains(input, target) || strings.Contains(target, input) {
		return true
	}

	// Check for common variations
	variations := s.getBrandVariations(target)
	for _, variation := range variations {
		if input == variation {
			return true
		}
		if strings.Contains(input, variation) || strings.Contains(variation, input) {
			return true
		}
	}

	return false
}

// getBrandVariations returns common variations of a brand name
func (s *BrandService) getBrandVariations(brandName string) []string {
	variations := []string{}

	// Add common abbreviations and variations
	switch brandName {
	case "cocacola":
		variations = append(variations, "coca", "cola", "coke")
	case "redbull":
		variations = append(variations, "red", "bull", "redbullenergy")
	case "nike":
		variations = append(variations, "nikeshoes", "nikeinc")
	case "adidas":
		variations = append(variations, "adidasshoes", "adidasgroup")
	case "pepsi":
		variations = append(variations, "pepsico", "pepsicola")
	case "mcdonalds":
		variations = append(variations, "mcd", "mcdonaldsrestaurant")
	case "starbucks":
		variations = append(variations, "starbuckscoffee")
	case "apple":
		variations = append(variations, "appleinc", "appletechnology")
	case "samsung":
		variations = append(variations, "samsungelectronics")
	case "microsoft":
		variations = append(variations, "microsoftcorporation")
	}

	return variations
}

// GetBrandDisplayName returns a display-friendly name for a brand
func (s *BrandService) GetBrandDisplayName(brandName string) string {
	// Convert to title case and handle common cases
	displayName := s.toTitleCase(brandName)

	// Handle specific brand name formatting
	switch strings.ToLower(brandName) {
	case "coca-cola":
		return "Coca-Cola"
	case "redbull":
		return "Red Bull"
	case "nike":
		return "Nike"
	case "adidas":
		return "Adidas"
	case "pepsi":
		return "Pepsi"
	case "mcdonalds":
		return "McDonald's"
	case "starbucks":
		return "Starbucks"
	case "apple":
		return "Apple"
	case "samsung":
		return "Samsung"
	case "microsoft":
		return "Microsoft"
	}

	return displayName
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
