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

// ParseAnalysis parses the OpenAI response and extracts analysis fields
func ParseAnalysis(response string) (*AnalysisResult, error) {
	// Clean the response
	cleaned := strings.TrimSpace(response)

	// Try to parse as JSON first
	var result AnalysisResult
	err := json.Unmarshal([]byte(cleaned), &result)
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
