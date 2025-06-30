package parser

import (
	"encoding/json"
	"errors"
	"strings"
)

// AnalysisResult represents the parsed analysis from OpenAI
type AnalysisResult struct {
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	LitterProbability float64 `json:"litter_probability"`
	HazardProbability float64 `json:"hazard_probability"`
	SeverityLevel     float64 `json:"severity_level"`
}

// extractJSONFromMarkdown extracts JSON from markdown code blocks
func extractJSONFromMarkdown(response string) string {
	// Look for JSON code blocks with ``` markers
	startMarker := "```"
	endMarker := "```"

	startIdx := strings.Index(response, startMarker)
	if startIdx == -1 {
		// No code block found, try to find JSON object directly
		startIdx = strings.Index(response, "{")
		if startIdx == -1 {
			return response
		}
		endIdx := strings.LastIndex(response, "}")
		if endIdx == -1 {
			return response
		}
		return strings.TrimSpace(response[startIdx : endIdx+1])
	}

	// Find the end of the first code block
	endIdx := strings.Index(response[startIdx+len(startMarker):], endMarker)
	if endIdx == -1 {
		return response
	}
	endIdx += startIdx + len(startMarker)

	// Extract content between the markers
	content := response[startIdx+len(startMarker) : endIdx]

	// Remove the language identifier if present (e.g., "json")
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > 0 && (strings.TrimSpace(lines[0]) == "json" || strings.TrimSpace(lines[0]) == "") {
		content = strings.Join(lines[1:], "\n")
	}

	return strings.TrimSpace(content)
}

// ParseAnalysis parses the OpenAI response and extracts analysis fields
func ParseAnalysis(response string) (*AnalysisResult, error) {
	// Clean the response
	cleaned := strings.TrimSpace(response)

	// Extract JSON from markdown if present
	jsonContent := extractJSONFromMarkdown(cleaned)

	// Try to parse as JSON
	var result AnalysisResult
	err := json.Unmarshal([]byte(jsonContent), &result)
	if err == nil {
		// Validate the parsed result
		if result.Title == "" {
			return nil, errors.New("title is required")
		}
		if result.Description == "" {
			return nil, errors.New("description is required")
		}
		if result.LitterProbability < 0 || result.LitterProbability > 1 {
			return nil, errors.New("litter_probability must be between 0 and 1")
		}
		if result.HazardProbability < 0 || result.HazardProbability > 1 {
			return nil, errors.New("hazard_probability must be between 0 and 1")
		}
		if result.SeverityLevel < 0 || result.SeverityLevel > 1 {
			return nil, errors.New("severity_level must be between 0 and 1")
		}
		return &result, nil
	}

	// If JSON parsing fails, return error
	return nil, errors.New("failed to parse JSON response: " + err.Error())
}
