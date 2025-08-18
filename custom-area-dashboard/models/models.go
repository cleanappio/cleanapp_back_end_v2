package models

import (
	"encoding/json"
	"time"
)

// GeoJSON structures
type FeatureCollection struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

type Feature struct {
	Type       string                 `json:"type"`
	Geometry   Geometry               `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// CustomArea represents a parsed area from the GeoJSON
type CustomArea struct {
	AreaID int64           `json:"area_id,omitempty"`
	Area   json.RawMessage `json:"area"` // Raw area data
	Name   string          `json:"name,omitempty"`
}

// AreasResponse represents the response for areas by admin level
type AreasResponse struct {
	Count int          `json:"count"`
	Areas []CustomArea `json:"areas"`
}

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

// ReportsResponse represents the response for reports within a custom area
type ReportsResponse struct {
	Reports []ReportWithAnalysis `json:"reports"`
	Count   int                  `json:"count"`
}

// AreaAggrData represents aggregated data for a single area
type AreaAggrData struct {
	AreaID                int64   `json:"area_id"`
	Name                  string  `json:"name"`
	ReportsCount          int     `json:"reports_count"`
	ReportsMean           float64 `json:"reports_mean"`
	ReportsMax            int     `json:"reports_max"`
	MeanSeverity          float64 `json:"mean_severity"`
	MeanLitterProbability float64 `json:"mean_litter_probability"`
	MeanHazardProbability float64 `json:"mean_hazard_probability"`
}

// ReportsAggrResponse represents the response for aggregated reports data
type ReportsAggrResponse struct {
	Areas []AreaAggrData `json:"areas"`
	Count int            `json:"count"`
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

// ReportAuthorizationRequest represents a request to check report authorization
type ReportAuthorizationRequest struct {
	ReportSeqs []int `json:"report_seqs"`
}

// ReportAuthorization represents the authorization status for a single report
type ReportAuthorization struct {
	ReportSeq  int    `json:"report_seq"`
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason,omitempty"`
}

// ReportAuthorizationResponse represents the response for report authorization check
type ReportAuthorizationResponse struct {
	Authorizations []ReportAuthorization `json:"authorizations"`
}
