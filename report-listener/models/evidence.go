package models

import "time"

// PublicEvidence separates original observations, acquisition, and assistive analysis.
// Nil values mean unknown; they are never inferred from ingestion or case closure.
type PublicEvidence struct {
	SubmissionTransport     *string                 `json:"submission_transport,omitempty"`
	SubmissionClient        *string                 `json:"submission_client,omitempty"`
	SubmissionClientClaimed bool                    `json:"submission_client_claimed"`
	ReporterContext         *string                 `json:"reporter_context,omitempty"`
	ReportedProductOrSite   *string                 `json:"reported_product_or_site,omitempty"`
	ReferencedURLs          []string                `json:"referenced_urls"`
	SourceReferences        []PublicSourceReference `json:"source_references"`
	SourceReferenceCount    *int                    `json:"source_reference_count,omitempty"`
	ObservedAtBasis         string                  `json:"observed_at_basis"`
	SourcePublishedAtBasis  string                  `json:"source_published_at_basis"`
	ReportKind              string                  `json:"report_kind"`
	IntakeMethod            string                  `json:"intake_method"`
	IntakeChannel           *string                 `json:"intake_channel"`
	IntakeLabel             string                  `json:"intake_label"`
	SourcePlatform          *string                 `json:"source_platform"`
	SourceURL               *string                 `json:"source_url"`
	SourcePublishedAt       *time.Time              `json:"source_published_at"`
	ObservedAt              *time.Time              `json:"observed_at"`
	IngestedAt              *time.Time              `json:"ingested_at"`
	Excerpt                 *string                 `json:"excerpt"`
	ReportedImpact          *string                 `json:"reported_impact"`
	ClassificationReason    *string                 `json:"classification_reason"`
	EntityRelationship      string                  `json:"entity_relationship"`
	ValidationStatus        string                  `json:"validation_status"`
	UpstreamStatus          string                  `json:"upstream_status"`
	ResolutionStatus        string                  `json:"resolution_status"`
	WhySurfaced             []string                `json:"why_surfaced"`
	HasOriginalMedia        bool                    `json:"has_original_media"`
	EvidenceVersion         int                     `json:"evidence_version"`
}

// References are submitted links, not independent users or verified accounts.
type PublicSourceReference struct {
	URL      string `json:"url"`
	Kind     string `json:"kind"`
	Platform string `json:"platform,omitempty"`
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

// EvidenceOverview counts visible report records before source/case grouping.
// Time windows describe additions to CleanApp, not when an incident happened.
type EvidenceOverview struct {
	TotalReports         int        `json:"total_reports"`
	ReportsLast30Days    int        `json:"reports_last_30_days"`
	ReportsLast7Days     int        `json:"reports_last_7_days"`
	ReportsPrevious7Days int        `json:"reports_previous_7_days"`
	LatestReportAt       *time.Time `json:"latest_report_at"`
	AsOf                 time.Time  `json:"as_of"`
	Classification       string     `json:"classification"`
	DateBasis            string     `json:"date_basis"`
}
