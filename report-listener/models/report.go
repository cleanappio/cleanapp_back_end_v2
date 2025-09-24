package models

import (
	"time"
)

// Report represents a report from the reports table
type Report struct {
	Seq       int       `json:"seq" db:"seq"`
	Timestamp time.Time `json:"timestamp" db:"ts"`
	ID        string    `json:"id" db:"id"`
	Latitude  float64   `json:"latitude" db:"latitude"`
	Longitude float64   `json:"longitude" db:"longitude"`
	Image     []byte    `json:"image,omitempty" db:"image"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq               int       `json:"seq" db:"seq"`
	Source            string    `json:"source" db:"source"`
	AnalysisText      string    `json:"analysis_text" db:"analysis_text"`
	AnalysisImage     []byte    `json:"analysis_image,omitempty" db:"analysis_image"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	BrandName         string    `json:"brand_name" db:"brand_name"`
	BrandDisplayName  string    `json:"brand_display_name" db:"brand_display_name"`
	LitterProbability float64   `json:"litter_probability" db:"litter_probability"`
	HazardProbability float64   `json:"hazard_probability" db:"hazard_probability"`
	DigitalBugProbability float64   `json:"digital_bug_probability" db:"digital_bug_probability"`
	SeverityLevel     float64   `json:"severity_level" db:"severity_level"`
	Summary           string    `json:"summary" db:"summary"`
	Language          string    `json:"language" db:"language"`
	Classification    string    `json:"classification" db:"classification"`
	IsValid           bool      `json:"is_valid" db:"is_valid"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// MinimalAnalysis represents only the essential analysis fields for lite API responses
type MinimalAnalysis struct {
	SeverityLevel  float64 `json:"severity_level"`
	Classification string  `json:"classification"`
	Language       string  `json:"language"`
	Title          string  `json:"title"`
}

// ReportWithMinimalAnalysis represents a report with minimal analysis data
type ReportWithMinimalAnalysis struct {
	Report   Report            `json:"report"`
	Analysis []MinimalAnalysis `json:"analysis"`  // Array of minimal analysis objects
}

// ReportWithAnalysis represents a report with its corresponding analysis
type ReportWithAnalysis struct {
	Report   Report           `json:"report"`
	Analysis []ReportAnalysis `json:"analysis"`
}

// ReportBatch represents a batch of reports to be broadcasted
type ReportBatch struct {
	Reports []ReportWithAnalysis `json:"reports"`
	Count   int                  `json:"count"`
	FromSeq int                  `json:"from_seq"`
	ToSeq   int                  `json:"to_seq"`
}

// BroadcastMessage represents a message sent to WebSocket clients
type BroadcastMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status           string `json:"status"`
	Service          string `json:"service"`
	Timestamp        string `json:"timestamp"`
	ConnectedClients int    `json:"connected_clients"`
	LastBroadcastSeq int    `json:"last_broadcast_seq"`
}
