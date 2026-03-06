package events

import (
	"encoding/json"
	"fmt"
	"time"
)

const ReportAnalysedVersion = "v1"

type ReportAnalysedReport struct {
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	ID          string    `json:"id"`
	Team        int       `json:"team"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	ActionID    string    `json:"action_id"`
	Description string    `json:"description"`
}

type ReportAnalysedAnalysis struct {
	Seq                   int       `json:"seq"`
	Source                string    `json:"source"`
	AnalysisText          string    `json:"analysis_text"`
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
	LegalRiskEstimate     string    `json:"legal_risk_estimate,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ReportAnalysed struct {
	Version  string                   `json:"version"`
	Report   ReportAnalysedReport     `json:"report"`
	Analysis []ReportAnalysedAnalysis `json:"analysis"`
}

func (e *ReportAnalysed) Normalize() {
	if e.Version == "" {
		e.Version = ReportAnalysedVersion
	}
}

func DecodeReportAnalysed(body []byte) (*ReportAnalysed, error) {
	var event ReportAnalysed
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, err
	}
	event.Normalize()
	if event.Report.Seq <= 0 {
		return nil, fmt.Errorf("missing report seq")
	}
	if len(event.Analysis) == 0 {
		return nil, fmt.Errorf("missing analysis")
	}
	return &event, nil
}
