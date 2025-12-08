package models

import (
	"time"
)

// Report represents a report from the database
type Report struct {
	Seq       int64     `json:"seq"`
	ID        string    `json:"id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Image     []byte    `json:"image"`
	Timestamp time.Time `json:"timestamp"`
}

// ReportAnalysis represents analysis data for a report
type ReportAnalysis struct {
	Seq                   int64   `json:"seq"`
	Source                string  `json:"source"`
	Title                 string  `json:"title"`
	Description           string  `json:"description"`
	BrandName             string  `json:"brand_name"`
	BrandDisplayName      string  `json:"brand_display_name"`
	LitterProbability     float64 `json:"litter_probability"`
	HazardProbability     float64 `json:"hazard_probability"`
	SeverityLevel         float64 `json:"severity_level"`
	Summary               string  `json:"summary"`
	InferredContactEmails string  `json:"inferred_contact_emails"`
	Classification        string  `json:"classification"`
	LegalRiskEstimate     string  `json:"legal_risk_estimate"`
}
