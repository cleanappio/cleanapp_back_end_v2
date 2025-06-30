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
}

// ReportAnalysis represents analysis data from the report_analysis table
type ReportAnalysis struct {
	Seq           int       `json:"seq" db:"seq"`
	Source        string    `json:"source" db:"source"`
	AnalysisText  string    `json:"analysis_text" db:"analysis_text"`
	AnalysisImage []byte    `json:"analysis_image,omitempty" db:"analysis_image"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// ReportWithAnalysis represents a report with its corresponding analysis
type ReportWithAnalysis struct {
	Report   Report         `json:"report"`
	Analysis ReportAnalysis `json:"analysis"`
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
