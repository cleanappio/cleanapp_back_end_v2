package services

import (
	"log"
	"strings"
	"unicode"
)

type brandAliasRule struct {
	Normalized string
	Display    string
	Aliases    []string
}

var productAliasRules = []brandAliasRule{
	{Normalized: "claude", Display: "Claude", Aliases: []string{"claude", "claude ai", "claudeai", "@claudeai"}},
	{Normalized: "chatgpt", Display: "ChatGPT", Aliases: []string{"chatgpt"}},
	{Normalized: "grok", Display: "Grok", Aliases: []string{"grok"}},
	{Normalized: "gemini", Display: "Gemini", Aliases: []string{"gemini"}},
	{Normalized: "googlenotebooklm", Display: "Google NotebookLM", Aliases: []string{"notebooklm", "google notebooklm"}},
	{Normalized: "instagram", Display: "Instagram", Aliases: []string{"instagram"}},
	{Normalized: "facebook", Display: "Facebook", Aliases: []string{"facebook"}},
	{Normalized: "whatsapp", Display: "WhatsApp", Aliases: []string{"whatsapp"}},
}

// BrandService manages brand name normalization
type BrandService struct{}

// NewBrandService creates a new brand service
func NewBrandService() *BrandService {
	return &BrandService{}
}

// NormalizeBrandName normalizes a brand name for consistent storage
// This function handles common variations and standardizes brand names
func (s *BrandService) NormalizeBrandName(brandName string) string {
	normalized := normalizeBrandName(brandName)
	if brandName != "" {
		log.Printf("Normalizing brand name: %s -> %s", brandName, normalized)
	}
	return normalized
}

func normalizeBrandName(brandName string) string {
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

	return normalized
}

// ResolveBrand canonicalizes only the selected complaint subject. Raw text,
// screenshots and generated summaries cannot replace it with a mentioned brand.
// An unsupported subject remains unknown instead of being filled from context.
func (s *BrandService) ResolveBrand(brandName string, _ ...string) (string, string) {
	normalized := s.NormalizeBrandName(brandName)
	if normalized == "" || normalized == "null" {
		return "", ""
	}
	for _, rule := range productAliasRules {
		if normalized == rule.Normalized || matchesNormalizedAlias(normalized, rule.Aliases) {
			return rule.Normalized, rule.Display
		}
	}
	return normalized, s.GetBrandDisplayName(brandName)
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
	case "cleanapp":
		return "CleanApp"
	case "cocacola", "coca-cola":
		return "Coca-Cola"
	case "redbull", "red bull":
		return "Red Bull"
	case "mcdonalds", "mcdonald's":
		return "McDonald's"
	case "claude":
		return "Claude"
	case "chatgpt":
		return "ChatGPT"
	case "grok":
		return "Grok"
	case "gemini":
		return "Gemini"
	case "googlenotebooklm":
		return "Google NotebookLM"
	case "instagram":
		return "Instagram"
	case "whatsapp":
		return "WhatsApp"
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

func matchesNormalizedAlias(normalized string, aliases []string) bool {
	for _, alias := range aliases {
		if normalized == normalizeBrandName(alias) {
			return true
		}
	}
	return false
}
