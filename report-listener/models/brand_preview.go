package models

import "time"

// PublicBrandPreview is a bounded sample, never a brand inventory or count.
type PublicBrandPreview struct {
	BrandFound     bool                       `json:"brand_found"`
	BrandName      string                     `json:"brand_name"`
	BrandDisplay   string                     `json:"brand_display_name"`
	Classification string                     `json:"classification"`
	EvidenceKind   string                     `json:"evidence_kind"`
	Reports        []PublicBrandPreviewReport `json:"reports"`
}

type PublicBrandPreviewReport struct {
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
	SourcePlatform          *string                 `json:"source_platform,omitempty"`
	PublicID                string                  `json:"public_id"`
	CanonicalPath           string                  `json:"canonical_path"`
	Title                   string                  `json:"title"`
	Summary                 string                  `json:"summary"`
	Language                string                  `json:"language"`
	ReportKind              string                  `json:"report_kind"`
	SourceURL               *string                 `json:"source_url"`
	SourcePublishedAt       *time.Time              `json:"source_published_at"`
	Excerpt                 *string                 `json:"excerpt"`
	IntakeMethod            string                  `json:"intake_method"`
	IntakeChannel           *string                 `json:"intake_channel"`
	ObservedAt              *time.Time              `json:"observed_at"`
	IngestedAt              *time.Time              `json:"ingested_at"`
}
