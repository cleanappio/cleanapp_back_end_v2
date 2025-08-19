package models

import "time"

// Report represents a report from the reports table
type Report struct {
	Seq       int       `json:"seq" db:"seq"`
	Timestamp time.Time `json:"timestamp" db:"ts"`
	ID        string    `json:"id" db:"id"`
	Latitude  float64   `json:"latitude" db:"latitude"`
	Longitude float64   `json:"longitude" db:"longitude"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq              int    `json:"seq" db:"seq"`
	BrandName        string `json:"brand_name" db:"brand_name"`
	BrandDisplayName string `json:"brand_display_name" db:"brand_display_name"`
}

// ReportWithAnalysis represents a report with its analysis data
type ReportWithAnalysis struct {
	Report   Report         `json:"report"`
	Analysis ReportAnalysis `json:"analysis"`
}

// ReportOwner represents ownership information for a report
type ReportOwner struct {
	Seq   int    `json:"seq" db:"seq"`
	Owner string `json:"owner" db:"owner"`
}

// ReportOwnership represents the complete ownership information for a report
type ReportOwnership struct {
	Seq     int      `json:"seq"`
	Owners  []string `json:"owners"`
	Reasons []string `json:"reasons"`
}

// ServiceStatus represents the current status of the ownership service
type ServiceStatus struct {
	Status           string    `json:"status"`
	LastProcessedSeq int       `json:"last_processed_seq"`
	TotalReports     int       `json:"total_reports"`
	LastUpdate       time.Time `json:"last_update"`
}
