package models

import (
	"time"
)

// ReportStatus represents a report status entry
type ReportStatus struct {
	Seq       int       `json:"seq" db:"seq"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// MarkResolvedRequest represents the request to mark a report as resolved
type MarkResolvedRequest struct {
	Seq int `json:"seq" binding:"required"`
}

// MarkResolvedResponse represents the response for marking a report as resolved
type MarkResolvedResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Seq     int    `json:"seq"`
	Status  string `json:"status"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Timestamp string `json:"timestamp"`
}

// MatchReportRequest represents the request to match a report
type MatchReportRequest struct {
	Version    string   `json:"version" binding:"required"`
	ID         string   `json:"id" binding:"required"`
	Latitude   float64  `json:"latitude" binding:"required"`
	Longitude  float64  `json:"longitude" binding:"required"`
	X          float64  `json:"x" binding:"required"`
	Y          float64  `json:"y" binding:"required"`
	Image      []byte   `json:"image" binding:"required"`
	Annotation string   `json:"annotation"`
	Tags       []string `json:"tags,omitempty"`
}

// Report represents a report from the database
type Report struct {
	Seq          int     `json:"seq" db:"seq"`
	ID           string  `json:"id" db:"id"`
	Team         int     `json:"team" db:"team"`
	Latitude     float64 `json:"latitude" db:"latitude"`
	Longitude    float64 `json:"longitude" db:"longitude"`
	X            float64 `json:"x" db:"x"`
	Y            float64 `json:"y" db:"y"`
	Image        []byte  `json:"image" db:"image"`
	ActionID     *string `json:"action_id" db:"action_id"`
	AnalysisText string  `json:"analysis_text" db:"analysis_text"`
}

// MatchResult represents the result of comparing two images
type MatchResult struct {
	ReportSeq  int     `json:"report_seq"`
	Similarity float64 `json:"similarity"`
	Resolved   bool    `json:"resolved"`
}

// MatchReportResponse represents the response for matching a report
type MatchReportResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Results []MatchResult `json:"results"`
}

// Response represents a response from the database
type Response struct {
	Seq       int     `json:"seq" db:"seq"`
	ID        string  `json:"id" db:"id"`
	Team      int     `json:"team" db:"team"`
	Latitude  float64 `json:"latitude" db:"latitude"`
	Longitude float64 `json:"longitude" db:"longitude"`
	X         float64 `json:"x" db:"x"`
	Y         float64 `json:"y" db:"y"`
	Image     []byte  `json:"image" db:"image"`
	ActionID  *string `json:"action_id" db:"action_id"`
	Status    string  `json:"status" db:"status"`
	ReportSeq int     `json:"report_seq" db:"report_seq"`
}
