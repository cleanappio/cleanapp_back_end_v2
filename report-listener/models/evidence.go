package models

import "time"

// PublicEvidence separates original observations, acquisition, and assistive analysis.
// Nil values mean unknown; they are never inferred from ingestion or case closure.
type PublicEvidence struct {
	ReportKind           string     `json:"report_kind"`
	IntakeMethod         string     `json:"intake_method"`
	IntakeLabel          string     `json:"intake_label"`
	SourcePlatform       *string    `json:"source_platform"`
	SourceURL            *string    `json:"source_url"`
	SourcePublishedAt    *time.Time `json:"source_published_at"`
	ObservedAt           *time.Time `json:"observed_at"`
	IngestedAt           time.Time  `json:"ingested_at"`
	Excerpt              *string    `json:"excerpt"`
	ReportedImpact       *string    `json:"reported_impact"`
	ClassificationReason *string    `json:"classification_reason"`
	EntityRelationship   string     `json:"entity_relationship"`
	ValidationStatus     string     `json:"validation_status"`
	UpstreamStatus       string     `json:"upstream_status"`
	ResolutionStatus     string     `json:"resolution_status"`
	WhySurfaced          []string   `json:"why_surfaced"`
	HasOriginalMedia     bool       `json:"has_original_media"`
	EvidenceVersion      int        `json:"evidence_version"`
}

// Related case membership is contextual, not proof of an identical defect or
// independent corroboration. Duplicate imports never become affected users.
type PublicSupportingSource struct {
	Title             string     `json:"title"`
	SourceURL         *string    `json:"source_url"`
	ReportKind        string     `json:"report_kind"`
	SourcePublishedAt *time.Time `json:"source_published_at"`
}

type PublicRecurrence struct {
	LatestObservedAt        *time.Time               `json:"latest_observed_at"`
	LatestSourcePublishedAt *time.Time               `json:"latest_source_published_at"`
	Grouping                string                   `json:"grouping"`
	SupportingSources       []PublicSupportingSource `json:"supporting_sources"`
	IssueID                 string                   `json:"issue_id"`
	CaseID                  *string                  `json:"case_id"`
	SupportingReportCount   int                      `json:"supporting_report_count"`
	SourceAccountCount      *int                     `json:"source_account_count"`
	DuplicateImportCount    int                      `json:"duplicate_import_count"`
	Independence            string                   `json:"independence"`
	RelatedObservationCount *int                     `json:"related_observation_count"`
	CountsExact             bool                     `json:"counts_exact"`
}

type EvidenceKindCounts struct {
	Firsthand int `json:"firsthand"`
	Coverage  int `json:"coverage"`
	Unknown   int `json:"unknown"`
	All       int `json:"all"`
}

type EvidenceScope struct {
	BrandName        string   `json:"brand_name"`
	Product          *string  `json:"product"`
	IncludesProducts bool     `json:"includes_products"`
	Products         []string `json:"products"`
}
