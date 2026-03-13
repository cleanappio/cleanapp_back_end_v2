package models

import (
	"time"
)

// Report represents a report from the reports table
type Report struct {
	Seq             int        `json:"seq" db:"seq"`
	PublicID        string     `json:"public_id" db:"public_id"`
	Timestamp       time.Time  `json:"timestamp" db:"ts"`
	ID              string     `json:"id" db:"id"`
	Team            int        `json:"team" db:"team"`
	Latitude        float64    `json:"latitude" db:"latitude"`
	Longitude       float64    `json:"longitude" db:"longitude"`
	X               float64    `json:"x" db:"x"`
	Y               float64    `json:"y" db:"y"`
	Image           []byte     `json:"image,omitempty" db:"image"`
	ActionID        *string    `json:"action_id,omitempty" db:"action_id"`
	Description     *string    `json:"description,omitempty" db:"description"`
	LastEmailSentAt *time.Time `json:"last_email_sent_at" db:"last_email_sent_at"`
	SourceTimestamp *time.Time `json:"source_timestamp,omitempty" db:"source_timestamp"`
	SourceURL       *string    `json:"source_url,omitempty" db:"source_url"`
}

// ReportAnalysis represents an analysis result
type ReportAnalysis struct {
	Seq                   int       `json:"seq" db:"seq"`
	Source                string    `json:"source" db:"source"`
	AnalysisText          string    `json:"analysis_text" db:"analysis_text"`
	AnalysisImage         []byte    `json:"analysis_image,omitempty" db:"analysis_image"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	BrandName             string    `json:"brand_name" db:"brand_name"`
	BrandDisplayName      string    `json:"brand_display_name" db:"brand_display_name"`
	LitterProbability     float64   `json:"litter_probability" db:"litter_probability"`
	HazardProbability     float64   `json:"hazard_probability" db:"hazard_probability"`
	DigitalBugProbability float64   `json:"digital_bug_probability" db:"digital_bug_probability"`
	SeverityLevel         float64   `json:"severity_level" db:"severity_level"`
	Summary               string    `json:"summary" db:"summary"`
	Language              string    `json:"language" db:"language"`
	Classification        string    `json:"classification" db:"classification"`
	InferredContactEmails string    `json:"inferred_contact_emails" db:"inferred_contact_emails"`
	IsValid               bool      `json:"is_valid" db:"is_valid"`
	CreatedAt             time.Time `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

// MinimalAnalysis represents only the essential analysis fields for lite API responses
type MinimalAnalysis struct {
	SeverityLevel    float64 `json:"severity_level"`
	Classification   string  `json:"classification"`
	Language         string  `json:"language"`
	Title            string  `json:"title"`
	Description      string  `json:"description,omitempty"`
	Summary          string  `json:"summary"`
	BrandName        string  `json:"brand_name,omitempty"`
	BrandDisplayName string  `json:"brand_display_name,omitempty"`
}

// ReportWithMinimalAnalysis represents a report with minimal analysis data
type ReportWithMinimalAnalysis struct {
	Report   Report            `json:"report"`
	Analysis []MinimalAnalysis `json:"analysis"` // Array of minimal analysis objects
}

// ReportWithAnalysis represents a report with its corresponding analysis
type ReportWithAnalysis struct {
	Report               Report                   `json:"report"`
	Analysis             []ReportAnalysis         `json:"analysis"`
	EscalationTargets    []CaseEscalationTarget   `json:"escalation_targets,omitempty"`
	ContactObservations  []CaseContactObservation `json:"contact_observations,omitempty"`
	NotifyPlan           *CaseNotifyPlan          `json:"notify_plan,omitempty"`
	RoutingProfile       *SubjectRoutingProfile   `json:"routing_profile,omitempty"`
	ExecutionTasks       []NotifyExecutionTask    `json:"execution_tasks,omitempty"`
	NotifyOutcomes       []NotifyOutcome          `json:"notify_outcomes,omitempty"`
	ContactStrategyStale bool                     `json:"contact_strategy_stale,omitempty"`
}

type ReportContactStrategyResponse struct {
	ReportSeq            int                      `json:"report_seq"`
	PublicID             string                   `json:"public_id"`
	EscalationTargets    []CaseEscalationTarget   `json:"escalation_targets"`
	ContactObservations  []CaseContactObservation `json:"contact_observations"`
	NotifyPlan           *CaseNotifyPlan          `json:"notify_plan,omitempty"`
	RoutingProfile       *SubjectRoutingProfile   `json:"routing_profile,omitempty"`
	ExecutionTasks       []NotifyExecutionTask    `json:"execution_tasks,omitempty"`
	NotifyOutcomes       []NotifyOutcome          `json:"notify_outcomes,omitempty"`
	Refreshed            bool                     `json:"refreshed"`
	ContactStrategyStale bool                     `json:"contact_strategy_stale,omitempty"`
}

// ReportsByGeometryRequest selects reports inside a polygonal geometry.
// Geometry may be a GeoJSON Geometry object or a GeoJSON Feature wrapper.
type ReportsByGeometryRequest struct {
	Geometry       interface{} `json:"geometry"`
	Classification string      `json:"classification"`
	N              int         `json:"n"`
}

// ReportBatch represents a batch of reports to be broadcasted
type ReportBatch struct {
	Reports             []ReportWithAnalysis `json:"reports"`
	Count               int                  `json:"count"`
	TotalCount          int                  `json:"total_count,omitempty"`
	HighPriorityCount   int                  `json:"high_priority_count,omitempty"`
	MediumPriorityCount int                  `json:"medium_priority_count,omitempty"`
	FromSeq             int                  `json:"from_seq"`
	ToSeq               int                  `json:"to_seq"`
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
