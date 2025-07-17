package models

import (
	"time"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
	Service          string `json:"service,omitempty"`
	Timestamp        string `json:"timestamp,omitempty"`
	ConnectedClients int    `json:"connected_clients,omitempty"`
	LastBroadcastSeq int    `json:"last_broadcast_seq,omitempty"`
}

// ReportData represents a report from the database
type ReportData struct {
	Seq       int      `json:"seq"`
	Timestamp string   `json:"timestamp"`
	ID        string   `json:"id"`
	Team      int      `json:"team"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	ActionID  *string  `json:"action_id,omitempty"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq               int     `json:"seq"`
	Source            string  `json:"source"`
	AnalysisText      string  `json:"analysis_text"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	BrandName         string  `json:"brand_name"`
	BrandDisplayName  string  `json:"brand_display_name"`
	LitterProbability float64 `json:"litter_probability"`
	HazardProbability float64 `json:"hazard_probability"`
	SeverityLevel     float64 `json:"severity_level"`
	Summary           string  `json:"summary"`
	Language          string  `json:"language"`
	CreatedAt         string  `json:"created_at"`
}

// ReportWithAnalysis represents a report with its corresponding analysis
type ReportWithAnalysis struct {
	Report   ReportData       `json:"report"`
	Analysis []ReportAnalysis `json:"analysis"`
}

// ReportsResponse represents the response for reports with brand analysis
type ReportsResponse struct {
	Reports []ReportWithAnalysis `json:"reports"`
	Count   int                  `json:"count"`
	Brand   string               `json:"brand"`
}

// BrandInfo represents information about a brand
type BrandInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Count       int    `json:"count"`
}

// BrandsResponse represents the response for available brands
type BrandsResponse struct {
	Brands []BrandInfo `json:"brands"`
	Count  int         `json:"count"`
}

// ReportBatch represents a batch of reports to be broadcasted
type ReportBatch struct {
	Reports []ReportWithAnalysis `json:"reports"`
	Count   int                  `json:"count"`
	FromSeq int                  `json:"from_seq"`
	ToSeq   int                  `json:"to_seq"`
	Brand   string               `json:"brand"`
}

// BroadcastMessage represents a message sent to WebSocket clients
type BroadcastMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}
