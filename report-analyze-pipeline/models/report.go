package models

import (
	"time"
)

// Report represents a report from the reports table
type Report struct {
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	ID          string    `json:"id"`
	Team        int       `json:"team"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	Image       []byte    `json:"image,omitempty"`
	ActionID    string    `json:"action_id"`
	Description string    `json:"description"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq                   int       `json:"seq"`
	Source                string    `json:"source"`
	AnalysisText          string    `json:"analysis_text"`
	AnalysisImage         []byte    `json:"analysis_image,omitempty"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	BrandName             string    `json:"brand_name"`
	BrandDisplayName      string    `json:"brand_display_name"`
	LitterProbability     float64   `json:"litter_probability"`
	HazardProbability     float64   `json:"hazard_probability"`
	DigitalBugProbability float64   `json:"digital_bug_probability"`
	SeverityLevel         float64   `json:"severity_level"`
	Summary               string    `json:"summary"`
	Language              string    `json:"language"`
	Classification        string    `json:"classification"`
	IsValid               bool      `json:"is_valid"`
	InferredContactEmails string    `json:"inferred_contact_emails"`
	LegalRiskEstimate     string    `json:"legal_risk_estimate"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// ReportWithAnalysis represents a report with its corresponding analysis
type ReportWithAnalysis struct {
	Report   Report           `json:"report"`
	Analysis []ReportAnalysis `json:"analysis"`
}
