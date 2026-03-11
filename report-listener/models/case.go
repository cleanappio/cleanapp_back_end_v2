package models

import "time"

type ClusterFromReportRequest struct {
	Seq            int     `json:"seq"`
	RadiusKm       float64 `json:"radius_km"`
	N              int     `json:"n"`
	Classification string  `json:"classification"`
}

type ClusterIncidentHypothesis struct {
	HypothesisID            string   `json:"hypothesis_id"`
	Title                   string   `json:"title"`
	Classification          string   `json:"classification"`
	RepresentativeReportSeq int      `json:"representative_report_seq"`
	ReportSeqs              []int    `json:"report_seqs"`
	ReportCount             int      `json:"report_count"`
	Confidence              float64  `json:"confidence"`
	SeverityScore           float64  `json:"severity_score"`
	UrgencyScore            float64  `json:"urgency_score"`
	Rationale               []string `json:"rationale"`
}

type ClusterStats struct {
	Classification          string         `json:"classification"`
	ReportCount             int            `json:"report_count"`
	SeverityAverage         float64        `json:"severity_average"`
	SeverityMax             float64        `json:"severity_max"`
	HighPriorityCount       int            `json:"high_priority_count"`
	MediumPriorityCount     int            `json:"medium_priority_count"`
	FirstSeenAt             *time.Time     `json:"first_seen_at,omitempty"`
	LastSeenAt              *time.Time     `json:"last_seen_at,omitempty"`
	ClassificationBreakdown map[string]int `json:"classification_breakdown"`
}

type ClusterAnalysisResponse struct {
	ScopeType        string                      `json:"scope_type"`
	Classification   string                      `json:"classification"`
	AnchorReportSeq  int                         `json:"anchor_report_seq,omitempty"`
	Geometry         interface{}                 `json:"geometry,omitempty"`
	Reports          []ReportWithAnalysis        `json:"reports"`
	Stats            ClusterStats                `json:"stats"`
	Hypotheses       []ClusterIncidentHypothesis `json:"hypotheses"`
	SuggestedTargets []CaseEscalationTarget      `json:"suggested_targets"`
	CandidateCases   []CaseMatchCandidate        `json:"candidate_cases"`
}

type SavedCluster struct {
	ClusterID          string    `json:"cluster_id" db:"cluster_id"`
	SourceType         string    `json:"source_type" db:"source_type"`
	Classification     string    `json:"classification" db:"classification"`
	GeometryJSON       string    `json:"geometry_json" db:"geometry_json"`
	BBoxJSON           string    `json:"bbox_json" db:"bbox_json"`
	CentroidLat        *float64  `json:"centroid_lat,omitempty" db:"centroid_lat"`
	CentroidLng        *float64  `json:"centroid_lng,omitempty" db:"centroid_lng"`
	ClusterFingerprint string    `json:"cluster_fingerprint" db:"cluster_fingerprint"`
	SeedReportSeq      *int      `json:"seed_report_seq,omitempty" db:"seed_report_seq"`
	ReportCount        int       `json:"report_count" db:"report_count"`
	Summary            string    `json:"summary" db:"summary"`
	StatsJSON          string    `json:"stats_json" db:"stats_json"`
	AnalysisJSON       string    `json:"analysis_json" db:"analysis_json"`
	CreatedByUserID    string    `json:"created_by_user_id" db:"created_by_user_id"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

type Case struct {
	CaseID                string     `json:"case_id" db:"case_id"`
	Slug                  string     `json:"slug" db:"slug"`
	Title                 string     `json:"title" db:"title"`
	Type                  string     `json:"type" db:"type"`
	Status                string     `json:"status" db:"status"`
	Classification        string     `json:"classification" db:"classification"`
	Summary               string     `json:"summary" db:"summary"`
	UncertaintyNotes      string     `json:"uncertainty_notes" db:"uncertainty_notes"`
	GeometryJSON          string     `json:"geometry_json" db:"geometry_json"`
	AggregateGeometryJSON string     `json:"aggregate_geometry_json" db:"aggregate_geometry_json"`
	AggregateBBoxJSON     string     `json:"aggregate_bbox_json" db:"aggregate_bbox_json"`
	AnchorReportSeq       *int       `json:"anchor_report_seq,omitempty" db:"anchor_report_seq"`
	AnchorLat             *float64   `json:"anchor_lat,omitempty" db:"anchor_lat"`
	AnchorLng             *float64   `json:"anchor_lng,omitempty" db:"anchor_lng"`
	BuildingID            *string    `json:"building_id,omitempty" db:"building_id"`
	ParcelID              *string    `json:"parcel_id,omitempty" db:"parcel_id"`
	SeverityScore         float64    `json:"severity_score" db:"severity_score"`
	UrgencyScore          float64    `json:"urgency_score" db:"urgency_score"`
	ConfidenceScore       float64    `json:"confidence_score" db:"confidence_score"`
	ExposureScore         float64    `json:"exposure_score" db:"exposure_score"`
	CriticalityScore      float64    `json:"criticality_score" db:"criticality_score"`
	TrendScore            float64    `json:"trend_score" db:"trend_score"`
	ClusterCount          int        `json:"cluster_count" db:"cluster_count"`
	LinkedReportCount     int        `json:"linked_report_count" db:"linked_report_count"`
	FirstSeenAt           *time.Time `json:"first_seen_at,omitempty" db:"first_seen_at"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty" db:"last_seen_at"`
	LastClusterAt         *time.Time `json:"last_cluster_at,omitempty" db:"last_cluster_at"`
	MergedIntoCaseID      *string    `json:"merged_into_case_id,omitempty" db:"merged_into_case_id"`
	CreatedByUserID       string     `json:"created_by_user_id" db:"created_by_user_id"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at" db:"updated_at"`
}

type CaseClusterLink struct {
	CaseID      string  `json:"case_id" db:"case_id"`
	ClusterID   string  `json:"cluster_id" db:"cluster_id"`
	MatchScore  float64 `json:"match_score" db:"match_score"`
	MatchReason string  `json:"match_reason" db:"match_reason"`
}

type CaseMatchCandidate struct {
	CaseID                string    `json:"case_id" db:"case_id"`
	Slug                  string    `json:"slug" db:"slug"`
	Title                 string    `json:"title" db:"title"`
	Status                string    `json:"status" db:"status"`
	Classification        string    `json:"classification" db:"classification"`
	Summary               string    `json:"summary" db:"summary"`
	GeometryJSON          string    `json:"geometry_json" db:"geometry_json"`
	AggregateGeometryJSON string    `json:"aggregate_geometry_json" db:"aggregate_geometry_json"`
	AggregateBBoxJSON     string    `json:"aggregate_bbox_json" db:"aggregate_bbox_json"`
	AnchorReportSeq       *int      `json:"anchor_report_seq,omitempty" db:"anchor_report_seq"`
	AnchorLat             *float64  `json:"anchor_lat,omitempty" db:"anchor_lat"`
	AnchorLng             *float64  `json:"anchor_lng,omitempty" db:"anchor_lng"`
	ClusterCount          int       `json:"cluster_count" db:"cluster_count"`
	LinkedReportCount     int       `json:"linked_report_count" db:"linked_report_count"`
	SharedReportCount     int       `json:"shared_report_count"`
	MatchScore            float64   `json:"match_score"`
	MatchReasons          []string  `json:"match_reasons"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
	LinkedReportSeqs      []int     `json:"-" db:"-"`
}

type CaseReportLink struct {
	CaseID          string     `json:"case_id" db:"case_id"`
	Seq             int        `json:"seq" db:"seq"`
	PublicID        string     `json:"public_id" db:"public_id"`
	LinkReason      string     `json:"link_reason" db:"link_reason"`
	Confidence      float64    `json:"confidence" db:"confidence"`
	AttachedAt      time.Time  `json:"attached_at" db:"attached_at"`
	Title           string     `json:"title" db:"title"`
	Summary         string     `json:"summary" db:"summary"`
	Classification  string     `json:"classification" db:"classification"`
	SeverityLevel   float64    `json:"severity_level" db:"severity_level"`
	Latitude        float64    `json:"latitude" db:"latitude"`
	Longitude       float64    `json:"longitude" db:"longitude"`
	ReportTimestamp time.Time  `json:"report_timestamp" db:"report_timestamp"`
	LastEmailSentAt *time.Time `json:"last_email_sent_at" db:"last_email_sent_at"`
	RecipientCount  int        `json:"recipient_count" db:"recipient_count"`
}

type CaseEscalationTarget struct {
	ID              int64     `json:"id" db:"id"`
	CaseID          string    `json:"case_id" db:"case_id"`
	RoleType        string    `json:"role_type" db:"role_type"`
	Organization    string    `json:"organization" db:"organization"`
	DisplayName     string    `json:"display_name" db:"display_name"`
	Channel         string    `json:"channel" db:"channel"`
	Email           string    `json:"email" db:"email"`
	Phone           string    `json:"phone" db:"phone"`
	Website         string    `json:"website" db:"website"`
	ContactURL      string    `json:"contact_url" db:"contact_url"`
	SocialPlatform  string    `json:"social_platform" db:"social_platform"`
	SocialHandle    string    `json:"social_handle" db:"social_handle"`
	TargetSource    string    `json:"target_source" db:"target_source"`
	ConfidenceScore float64   `json:"confidence_score" db:"confidence_score"`
	Rationale       string    `json:"rationale" db:"rationale"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

type CaseEscalationAction struct {
	ID                int64      `json:"id" db:"id"`
	CaseID            string     `json:"case_id" db:"case_id"`
	TargetID          *int64     `json:"target_id,omitempty" db:"target_id"`
	Channel           string     `json:"channel" db:"channel"`
	Status            string     `json:"status" db:"status"`
	Subject           string     `json:"subject" db:"subject"`
	Body              string     `json:"body" db:"body"`
	AttachmentsJSON   string     `json:"attachments_json" db:"attachments_json"`
	SentByUserID      string     `json:"sent_by_user_id" db:"sent_by_user_id"`
	ProviderMessageID string     `json:"provider_message_id" db:"provider_message_id"`
	SentAt            *time.Time `json:"sent_at,omitempty" db:"sent_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

type CaseEmailDelivery struct {
	ID                int64      `json:"id" db:"id"`
	CaseID            string     `json:"case_id" db:"case_id"`
	ActionID          *int64     `json:"action_id,omitempty" db:"action_id"`
	TargetID          *int64     `json:"target_id,omitempty" db:"target_id"`
	RecipientEmail    string     `json:"recipient_email" db:"recipient_email"`
	DeliveryStatus    string     `json:"delivery_status" db:"delivery_status"`
	DeliverySource    string     `json:"delivery_source" db:"delivery_source"`
	Provider          string     `json:"provider" db:"provider"`
	ProviderMessageID string     `json:"provider_message_id" db:"provider_message_id"`
	SentAt            *time.Time `json:"sent_at,omitempty" db:"sent_at"`
	ErrorMessage      string     `json:"error_message" db:"error_message"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

type CaseResolutionSignal struct {
	ID              int64     `json:"id" db:"id"`
	CaseID          string    `json:"case_id" db:"case_id"`
	SourceType      string    `json:"source_type" db:"source_type"`
	Summary         string    `json:"summary" db:"summary"`
	LinkedReportSeq *int      `json:"linked_report_seq,omitempty" db:"linked_report_seq"`
	MetadataJSON    string    `json:"metadata_json" db:"metadata_json"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

type CaseAuditEvent struct {
	ID          int64     `json:"id" db:"id"`
	CaseID      string    `json:"case_id" db:"case_id"`
	EventType   string    `json:"event_type" db:"event_type"`
	ActorUserID string    `json:"actor_user_id" db:"actor_user_id"`
	PayloadJSON string    `json:"payload_json" db:"payload_json"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type CaseDetail struct {
	Case              Case                   `json:"case"`
	LinkedReports     []CaseReportLink       `json:"linked_reports"`
	Clusters          []SavedCluster         `json:"clusters"`
	EscalationTargets []CaseEscalationTarget `json:"escalation_targets"`
	EscalationActions []CaseEscalationAction `json:"escalation_actions"`
	EmailDeliveries   []CaseEmailDelivery    `json:"email_deliveries"`
	ResolutionSignals []CaseResolutionSignal `json:"resolution_signals"`
	AuditEvents       []CaseAuditEvent       `json:"audit_events"`
}

type ReportCaseSummary struct {
	CaseID                string    `json:"case_id" db:"case_id"`
	Slug                  string    `json:"slug" db:"slug"`
	Title                 string    `json:"title" db:"title"`
	Status                string    `json:"status" db:"status"`
	Classification        string    `json:"classification" db:"classification"`
	Summary               string    `json:"summary" db:"summary"`
	SeverityScore         float64   `json:"severity_score" db:"severity_score"`
	UrgencyScore          float64   `json:"urgency_score" db:"urgency_score"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
	EscalationTargetCount int       `json:"escalation_target_count" db:"escalation_target_count"`
	DeliveryCount         int       `json:"delivery_count" db:"delivery_count"`
}

type ReportCasesResponse struct {
	Seq   int                 `json:"seq"`
	Cases []ReportCaseSummary `json:"cases"`
}

type CreateCaseEscalationTargetRequest struct {
	RoleType        string  `json:"role_type"`
	Organization    string  `json:"organization"`
	DisplayName     string  `json:"display_name"`
	Channel         string  `json:"channel"`
	Email           string  `json:"email"`
	Phone           string  `json:"phone"`
	Website         string  `json:"website"`
	ContactURL      string  `json:"contact_url"`
	SocialPlatform  string  `json:"social_platform"`
	SocialHandle    string  `json:"social_handle"`
	TargetSource    string  `json:"target_source"`
	ConfidenceScore float64 `json:"confidence_score"`
	Rationale       string  `json:"rationale"`
}

type CreateCaseRequest struct {
	Title             string                              `json:"title"`
	Type              string                              `json:"type"`
	Status            string                              `json:"status"`
	Classification    string                              `json:"classification"`
	Summary           string                              `json:"summary"`
	UncertaintyNotes  string                              `json:"uncertainty_notes"`
	Geometry          interface{}                         `json:"geometry"`
	AnchorReportSeq   int                                 `json:"anchor_report_seq"`
	ReportSeqs        []int                               `json:"report_seqs"`
	ExistingCaseID    string                              `json:"existing_case_id"`
	ForceNewCase      bool                                `json:"force_new_case"`
	ClusterSummary    string                              `json:"cluster_summary"`
	ClusterSourceType string                              `json:"cluster_source_type"`
	ClusterStats      interface{}                         `json:"cluster_stats"`
	ClusterAnalysis   interface{}                         `json:"cluster_analysis"`
	EscalationTargets []CreateCaseEscalationTargetRequest `json:"escalation_targets"`
}

type MatchClusterRequest struct {
	Geometry       interface{} `json:"geometry"`
	Classification string      `json:"classification"`
	ReportSeqs     []int       `json:"report_seqs"`
	Title          string      `json:"title"`
	Summary        string      `json:"summary"`
	N              int         `json:"n"`
}

type MatchClusterResponse struct {
	Classification string               `json:"classification"`
	CandidateCases []CaseMatchCandidate `json:"candidate_cases"`
}

type MergeCasesRequest struct {
	TargetCaseID  string   `json:"target_case_id"`
	SourceCaseIDs []string `json:"source_case_ids"`
}

type AddReportsToCaseRequest struct {
	ReportSeqs []int   `json:"report_seqs"`
	LinkReason string  `json:"link_reason"`
	Confidence float64 `json:"confidence"`
}

type UpdateCaseStatusRequest struct {
	Status      string      `json:"status"`
	Summary     string      `json:"summary"`
	Payload     interface{} `json:"payload"`
	ActorUserID string      `json:"actor_user_id,omitempty"`
}

type DraftCaseEscalationRequest struct {
	TargetIDs []int64 `json:"target_ids"`
	Subject   string  `json:"subject"`
	Body      string  `json:"body"`
}

type SendCaseEscalationRequest struct {
	TargetIDs   []int64 `json:"target_ids"`
	Subject     string  `json:"subject"`
	Body        string  `json:"body"`
	ActorUserID string  `json:"actor_user_id,omitempty"`
}

type CaseEscalationDraftResponse struct {
	CaseID      string                 `json:"case_id"`
	Subject     string                 `json:"subject"`
	Body        string                 `json:"body"`
	Targets     []CaseEscalationTarget `json:"targets"`
	LinkedCount int                    `json:"linked_count"`
}

type CaseEscalationSendResponse struct {
	CaseID     string                 `json:"case_id"`
	Subject    string                 `json:"subject"`
	Body       string                 `json:"body"`
	Actions    []CaseEscalationAction `json:"actions"`
	Deliveries []CaseEmailDelivery    `json:"deliveries"`
}
