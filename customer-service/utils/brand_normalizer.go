package utils

import (
	"strings"
	"unicode"
)

// NormalizeBrandName normalizes a brand name for consistent storage and comparison
// This function removes common punctuation, converts to lowercase, and removes extra spaces
func NormalizeBrandName(brandName string) string {
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
	normalized = strings.ReplaceAll(normalized, "'", "")
	normalized = strings.ReplaceAll(normalized, "’", "")
	normalized = strings.ReplaceAll(normalized, "&", "")
	normalized = strings.ReplaceAll(normalized, "and", "")

	// Remove extra spaces
	normalized = strings.Join(strings.Fields(normalized), "")

	return normalized
}

// ToTitleCase converts a string to title case for display purposes
func ToTitleCase(str string) string {
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

// GetBrandDisplayName returns a display-friendly name for a brand
func GetBrandDisplayName(brandName string) string {
	// Convert to title case and handle common cases
	displayName := ToTitleCase(brandName)

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
