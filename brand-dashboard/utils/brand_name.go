package utils

import "strings"

// normalizeBrandName normalizes a brand name for comparison
func NormalizeBrandName(brandName string) string {
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
