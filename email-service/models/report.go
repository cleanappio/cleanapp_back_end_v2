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
	BrandReportCount      int     `json:"brand_report_count"` // Total reports for this brand
}

// BrandReportSummary represents aggregated report data for a brand
type BrandReportSummary struct {
	BrandName             string  `json:"brand_name"`
	BrandDisplayName      string  `json:"brand_display_name"`
	NewReportCount        int     `json:"new_report_count"`         // New reports since last notification
	TotalReportCount      int     `json:"total_report_count"`       // Total reports for this brand
	Classification        string  `json:"classification"`           // digital or physical
	InferredContactEmails string  `json:"inferred_contact_emails"`  // Comma-separated emails
	ReportSeqs            []int64 `json:"report_seqs"`              // Seqs of reports to mark as processed
	LatestReportSeq       int64   `json:"latest_report_seq"`        // Most recent report seq
}
