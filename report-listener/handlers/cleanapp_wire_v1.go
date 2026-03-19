package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"report-listener/config"
	"report-listener/database"
	"report-listener/publicid"

	"github.com/gin-gonic/gin"
)

const (
	cleanAppWireSchemaV1 = "cleanapp-wire.v1"

	wireLaneReject     = "reject"
	wireLaneQuarantine = "quarantine"
	wireLaneShadow     = "shadow"
	wireLanePublish    = "publish"
	wireLanePriority   = "priority"
	wireLaneHumanAuto  = "human_auto"
)

type cleanAppWireSubmission struct {
	SchemaVersion string `json:"schema_version"`
	SubmissionID  string `json:"submission_id,omitempty"`
	SourceID      string `json:"source_id"`
	SubmittedAt   string `json:"submitted_at"`
	ObservedAt    string `json:"observed_at,omitempty"`
	Agent         struct {
		AgentID         string `json:"agent_id"`
		AgentName       string `json:"agent_name,omitempty"`
		AgentType       string `json:"agent_type"`
		OperatorType    string `json:"operator_type,omitempty"`
		AuthMethod      string `json:"auth_method"`
		Signature       string `json:"signature,omitempty"`
		KeyID           string `json:"key_id,omitempty"`
		SoftwareVersion string `json:"software_version,omitempty"`
		ExecutionMode   string `json:"execution_mode,omitempty"`
	} `json:"agent"`
	Provenance struct {
		GenerationMethod string `json:"generation_method,omitempty"`
		UpstreamSources  []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"upstream_sources,omitempty"`
		ChainOfCustody    []string `json:"chain_of_custody,omitempty"`
		HumanInLoop       bool     `json:"human_in_loop,omitempty"`
		PromptFingerprint string   `json:"prompt_fingerprint,omitempty"`
		ModelInfo         struct {
			Provider    string  `json:"provider,omitempty"`
			Model       string  `json:"model,omitempty"`
			Temperature float64 `json:"temperature,omitempty"`
		} `json:"model_info,omitempty"`
	} `json:"provenance,omitempty"`
	Report struct {
		Domain         string  `json:"domain"`
		ProblemType    string  `json:"problem_type"`
		ProblemSubtype string  `json:"problem_subtype,omitempty"`
		Title          string  `json:"title"`
		Description    string  `json:"description,omitempty"`
		Language       string  `json:"language,omitempty"`
		Severity       string  `json:"severity,omitempty"`
		Confidence     float64 `json:"confidence"`
		Actionability  float64 `json:"actionability,omitempty"`
		TargetEntity   struct {
			TargetType       string `json:"target_type,omitempty"`
			Name             string `json:"name,omitempty"`
			JurisdictionHint string `json:"jurisdiction_hint,omitempty"`
		} `json:"target_entity,omitempty"`
		Location *struct {
			Kind            string  `json:"kind,omitempty"`
			Lat             float64 `json:"lat"`
			Lng             float64 `json:"lng"`
			Geohash         string  `json:"geohash,omitempty"`
			AddressText     string  `json:"address_text,omitempty"`
			PlaceConfidence float64 `json:"place_confidence,omitempty"`
		} `json:"location,omitempty"`
		DigitalContext any `json:"digital_context,omitempty"`
		EvidenceBundle []struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		} `json:"evidence_bundle,omitempty"`
		Tags             []string `json:"tags,omitempty"`
		SuggestedActions []string `json:"suggested_actions,omitempty"`
		Privacy          struct {
			ContainsPII       bool `json:"contains_pii,omitempty"`
			RequiresRedaction bool `json:"requires_redaction,omitempty"`
		} `json:"privacy,omitempty"`
		Economic struct {
			RewardClass               string  `json:"reward_class,omitempty"`
			EstimatedNovelty          float64 `json:"estimated_novelty,omitempty"`
			EstimatedVerificationCost float64 `json:"estimated_verification_cost,omitempty"`
		} `json:"economic,omitempty"`
	} `json:"report"`
	Dedupe struct {
		Fingerprint          string   `json:"fingerprint,omitempty"`
		NearDuplicateKeys    []string `json:"near_duplicate_keys,omitempty"`
		AgentClaimedOriginal bool     `json:"agent_claimed_original,omitempty"`
	} `json:"dedupe,omitempty"`
	Delivery struct {
		RequestedLane string `json:"requested_lane,omitempty"`
		WebhookURL    string `json:"webhook_url,omitempty"`
		WebhookAuth   string `json:"webhook_auth,omitempty"`
	} `json:"delivery,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type cleanAppWireBatchSubmission struct {
	Items []cleanAppWireSubmission `json:"items"`
}

type cleanAppWireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type cleanAppWireReceiptResponse struct {
	ReceiptID         string              `json:"receipt_id"`
	SubmissionID      string              `json:"submission_id"`
	SourceID          string              `json:"source_id"`
	ReceivedAt        string              `json:"received_at"`
	Status            string              `json:"status"`
	Lane              string              `json:"lane"`
	ReportID          int                 `json:"report_id,omitempty"`
	IdempotencyReplay bool                `json:"idempotency_replay"`
	Warnings          []string            `json:"warnings,omitempty"`
	Errors            []cleanAppWireError `json:"errors,omitempty"`
	NextCheckAfter    string              `json:"next_check_after,omitempty"`
	SubmissionQuality float64             `json:"submission_quality,omitempty"`
}

type cleanAppWireBatchResponse struct {
	Items      []cleanAppWireReceiptResponse `json:"items"`
	Submitted  int                           `json:"submitted"`
	Accepted   int                           `json:"accepted"`
	Rejected   int                           `json:"rejected"`
	Duplicates int                           `json:"duplicates"`
}

type cleanAppWireAgentProfileResponse struct {
	AgentID         string   `json:"agent_id"`
	AgentType       string   `json:"agent_type"`
	OperatorType    string   `json:"operator_type"`
	Status          string   `json:"status"`
	Tier            int      `json:"tier"`
	ReputationScore int      `json:"reputation_score"`
	Scopes          []string `json:"scopes"`
	Caps            struct {
		PerMinute int `json:"per_minute_cap_items"`
		Daily     int `json:"daily_cap_items"`
	} `json:"caps"`
	Usage struct {
		MinuteUsed     int `json:"minute_used"`
		DailyUsed      int `json:"daily_used"`
		DailyRemaining int `json:"daily_remaining"`
	} `json:"usage"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

type cleanAppWireAgentReputationResponse struct {
	AgentID            string  `json:"agent_id"`
	PrecisionScore     float64 `json:"precision_score"`
	NoveltyScore       float64 `json:"novelty_score"`
	EvidenceScore      float64 `json:"evidence_score"`
	RoutingScore       float64 `json:"routing_score"`
	CorroborationScore float64 `json:"corroboration_score"`
	LatencyScore       float64 `json:"latency_score"`
	ResolutionScore    float64 `json:"resolution_score"`
	PolicyScore        float64 `json:"policy_score"`
	DedupePenalty      float64 `json:"dedupe_penalty"`
	AbusePenalty       float64 `json:"abuse_penalty"`
	ReputationScore    float64 `json:"reputation_score"`
	SampleSize         int     `json:"sample_size"`
	Tier               int     `json:"tier"`
	Status             string  `json:"status"`
}

type cleanAppWireStatusResponse struct {
	SourceID          string `json:"source_id"`
	ReceiptID         string `json:"receipt_id"`
	SubmissionID      string `json:"submission_id"`
	Status            string `json:"status"`
	Lane              string `json:"lane"`
	ReportID          int    `json:"report_id,omitempty"`
	IdempotencyReplay bool   `json:"idempotency_replay"`
	UpdatedAt         string `json:"updated_at"`
}

type cleanAppWireAuthContext struct {
	FetcherID   string
	KeyID       string
	PerMinute   int
	Daily       int
	Tier        int
	Status      string
	Scopes      []string
	ActorKind   string
	Channel     string
	AuthMethod  string
	RiskScore   float64
	DisplayName string
}

type cleanAppWireIngestCoreMedia struct {
	URL         string
	SHA256      string
	ContentType string
}

type cleanAppWireIngestCoreItem struct {
	SourceID     string
	Title        string
	Description  string
	Lat          *float64
	Lng          *float64
	CollectedAt  string
	AgentID      string
	AgentVersion string
	SourceType   string
	ImageBase64  string
	Tags         []string
	Media        []cleanAppWireIngestCoreMedia
}

type cleanAppWireIngestCoreResult struct {
	SourceID   string
	Status     string
	ReportSeq  int
	Queued     bool
	Visibility string
	TrustLevel string
}

func (h *Handlers) RegisterAgentV1(c *gin.Context) {
	h.RegisterFetcherV1(c)
}

func (h *Handlers) GetAgentMeV1(c *gin.Context) {
	fetcherID, _ := c.Get(middlewareCtxKeyFetcherID())
	keyID, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	fid, _ := fetcherID.(string)
	kid, _ := keyID.(string)
	if fid == "" || kid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	key, fetcher, err := h.db.GetFetcherKeyAndFetcherV1(c.Request.Context(), kid)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	perMinute := fetcher.PerMinuteCapItems
	daily := fetcher.DailyCapItems
	if key.PerMinuteCapItems.Valid && int(key.PerMinuteCapItems.Int64) > 0 {
		perMinute = int(key.PerMinuteCapItems.Int64)
	}
	if key.DailyCapItems.Valid && int(key.DailyCapItems.Int64) > 0 {
		daily = int(key.DailyCapItems.Int64)
	}
	minUsed, dayUsed, _ := h.db.GetUsageV1(c.Request.Context(), fid, kid, time.Now())
	remaining := daily - dayUsed
	if remaining < 0 {
		remaining = 0
	}

	var resp cleanAppWireAgentProfileResponse
	resp.AgentID = fid
	resp.AgentType = "fetcher"
	resp.OperatorType = fetcher.OwnerType
	resp.Status = fetcher.Status
	resp.Tier = fetcher.Tier
	resp.ReputationScore = fetcher.ReputationScore
	resp.Scopes = key.Scopes
	resp.Caps.PerMinute = perMinute
	resp.Caps.Daily = daily
	resp.Usage.MinuteUsed = minUsed
	resp.Usage.DailyUsed = dayUsed
	resp.Usage.DailyRemaining = remaining
	if fetcher.LastSeenAt.Valid {
		resp.LastSeenAt = fetcher.LastSeenAt.Time.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) GetAgentReputationV1(c *gin.Context) {
	fetcherIDAny, _ := c.Get(middlewareCtxKeyFetcherID())
	currentFetcherID, _ := fetcherIDAny.(string)
	agentID := strings.TrimSpace(c.Param("agent_id"))
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id is required"})
		return
	}
	if agentID != currentFetcherID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	if err := h.db.EnsureWireReputationProfile(c.Request.Context(), agentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare reputation profile"})
		return
	}

	profile, err := h.db.GetWireReputationProfile(c.Request.Context(), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load reputation profile"})
		return
	}
	keyIDAny, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	keyID, _ := keyIDAny.(string)
	_, fetcher, err := h.db.GetFetcherKeyAndFetcherV1(c.Request.Context(), keyID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	resp := cleanAppWireAgentReputationResponse{
		AgentID:            agentID,
		PrecisionScore:     profile.PrecisionScore,
		NoveltyScore:       profile.NoveltyScore,
		EvidenceScore:      profile.EvidenceScore,
		RoutingScore:       profile.RoutingScore,
		CorroborationScore: profile.CorroborationScore,
		LatencyScore:       profile.LatencyScore,
		ResolutionScore:    profile.ResolutionScore,
		PolicyScore:        profile.PolicyScore,
		DedupePenalty:      profile.DedupePenalty,
		AbusePenalty:       profile.AbusePenalty,
		ReputationScore:    profile.ReputationScore,
		SampleSize:         profile.SampleSize,
		Tier:               fetcher.Tier,
		Status:             fetcher.Status,
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) SubmitCleanAppWireV1(c *gin.Context) {
	if !h.cfg.CleanAppWireEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "CleanApp Wire disabled"})
		return
	}

	auth, ok := h.cleanAppWireAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var sub cleanAppWireSubmission
	if err := c.ShouldBindJSON(&sub); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if err := h.db.ConsumeQuotaV1(c.Request.Context(), auth.FetcherID, auth.KeyID, time.Now(), 1, auth.PerMinute, auth.Daily); err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "quota_exceeded",
			"details": err.Error(),
		})
		return
	}

	resp, httpStatus := h.processCleanAppWireSubmission(c.Request.Context(), c, auth, sub)
	c.JSON(httpStatus, resp)
}

func (h *Handlers) BatchSubmitCleanAppWireV1(c *gin.Context) {
	if !h.cfg.CleanAppWireEnabled || !h.cfg.CleanAppWireBatchEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "CleanApp Wire batch submit disabled"})
		return
	}

	auth, ok := h.cleanAppWireAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req cleanAppWireBatchSubmission
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items is required"})
		return
	}
	if len(req.Items) > h.cfg.FetcherIngestMaxBatchItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("batch too large (max %d)", h.cfg.FetcherIngestMaxBatchItems)})
		return
	}

	if err := h.db.ConsumeQuotaV1(c.Request.Context(), auth.FetcherID, auth.KeyID, time.Now(), len(req.Items), auth.PerMinute, auth.Daily); err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "quota_exceeded",
			"details": err.Error(),
		})
		return
	}

	resp := cleanAppWireBatchResponse{Submitted: len(req.Items)}
	statusCode := http.StatusOK
	for _, item := range req.Items {
		receipt, code := h.processCleanAppWireSubmission(c.Request.Context(), c, auth, item)
		resp.Items = append(resp.Items, receipt)
		switch receipt.Status {
		case "rejected":
			resp.Rejected++
		default:
			resp.Accepted++
		}
		if receipt.IdempotencyReplay {
			resp.Duplicates++
		}
		if code >= 500 {
			statusCode = http.StatusServiceUnavailable
		}
	}

	c.JSON(statusCode, resp)
}

func (h *Handlers) GetCleanAppWireReceiptV1(c *gin.Context) {
	auth, ok := h.cleanAppWireAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	receiptID := strings.TrimSpace(c.Param("receipt_id"))
	if receiptID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "receipt_id is required"})
		return
	}
	receipt, err := h.db.GetWireReceipt(c.Request.Context(), auth.FetcherID, receiptID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "receipt not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load receipt"})
		return
	}
	c.JSON(http.StatusOK, h.cleanAppWireReceiptRecordToResponse(receipt, 0))
}

func (h *Handlers) GetCleanAppWireStatusV1(c *gin.Context) {
	auth, ok := h.cleanAppWireAuthContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	sourceID := strings.TrimSpace(c.Param("source_id"))
	if sourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_id is required"})
		return
	}
	receipt, err := h.db.GetLatestWireReceiptBySource(c.Request.Context(), auth.FetcherID, sourceID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "status not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load status"})
		return
	}
	resp := cleanAppWireStatusResponse{
		SourceID:          receipt.SourceID,
		ReceiptID:         receipt.ReceiptID,
		SubmissionID:      receipt.SubmissionID,
		Status:            receipt.Status,
		Lane:              receipt.Lane,
		IdempotencyReplay: receipt.IdempotencyReplay,
		UpdatedAt:         receipt.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if receipt.ReportSeq.Valid {
		resp.ReportID = int(receipt.ReportSeq.Int64)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) processCleanAppWireSubmission(ctx context.Context, c *gin.Context, auth cleanAppWireAuthContext, sub cleanAppWireSubmission) (cleanAppWireReceiptResponse, int) {
	return h.processCleanAppWireSubmissionInternal(ctx, auth, sub, c.ClientIP(), c.GetHeader("User-Agent"), c.GetHeader("X-Request-Id"), "/api/v1/agent-reports:submit")
}

func (h *Handlers) processCleanAppWireSubmissionInternal(ctx context.Context, auth cleanAppWireAuthContext, sub cleanAppWireSubmission, clientIP, userAgent, requestID, auditEndpoint string) (cleanAppWireReceiptResponse, int) {
	sub, generatedSubmissionID := normalizeCleanAppWireSubmission(sub)
	errors := validateCleanAppWireSubmission(sub, h.cfg.CleanAppWireStrictSignature)
	if len(errors) > 0 {
		return cleanAppWireReceiptResponse{
			ReceiptID:         newReceiptID(),
			SubmissionID:      generatedSubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors:            errors,
		}, http.StatusBadRequest
	}

	materialHash, err := cleanAppWireMaterialHash(sub)
	if err != nil {
		return cleanAppWireReceiptResponse{
			ReceiptID:         newReceiptID(),
			SubmissionID:      generatedSubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors: []cleanAppWireError{{
				Code:    "NORMALIZATION_FAILED",
				Message: "failed to normalize submission",
			}},
		}, http.StatusInternalServerError
	}

	if existing, err := h.db.GetWireSubmissionByFetcherAndSource(ctx, auth.FetcherID, sub.SourceID); err == nil {
		receipt, rerr := h.db.GetWireReceipt(ctx, auth.FetcherID, existing.ReceiptID)
		if rerr != nil {
			return cleanAppWireReceiptResponse{
				ReceiptID:         existing.ReceiptID,
				SubmissionID:      existing.SubmissionID,
				SourceID:          sub.SourceID,
				ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
				Status:            "accepted",
				Lane:              existing.Lane,
				IdempotencyReplay: true,
				Errors: []cleanAppWireError{{
					Code:    "RECEIPT_LOOKUP_FAILED",
					Message: "existing submission found but receipt lookup failed",
				}},
			}, http.StatusInternalServerError
		}
		if existing.MaterialHash == materialHash {
			resp := h.cleanAppWireReceiptRecordToResponse(receipt, existing.SubmissionQuality)
			resp.IdempotencyReplay = true
			return resp, http.StatusOK
		}
		return cleanAppWireReceiptResponse{
			ReceiptID:         newReceiptID(),
			SubmissionID:      generatedSubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors: []cleanAppWireError{{
				Code:    "REPLAY_CONFLICT",
				Message: "same source_id was previously submitted with a different payload",
			}},
		}, http.StatusConflict
	} else if err != sql.ErrNoRows {
		return cleanAppWireReceiptResponse{
			ReceiptID:         newReceiptID(),
			SubmissionID:      generatedSubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors: []cleanAppWireError{{
				Code:    "LOOKUP_FAILED",
				Message: "failed to check existing submission",
			}},
		}, http.StatusInternalServerError
	}

	quality := computeCleanAppWireSubmissionQuality(sub)
	lane := assignCleanAppWireLane(h.cfg, auth.Tier, quality, len(sub.Report.EvidenceBundle), strings.TrimSpace(sub.Delivery.RequestedLane))
	visibility := laneToVisibility(lane)
	trustLevel := laneToTrustLevel(lane)

	coreItem := cleanAppWireIngestCoreItemFromSubmission(sub)
	ingestResp, errCode, httpStatus := h.cleanAppWireIngestCore(ctx, auth, coreItem, visibility, trustLevel)
	if httpStatus >= 500 {
		return cleanAppWireReceiptResponse{
			ReceiptID:         newReceiptID(),
			SubmissionID:      sub.SubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        time.Now().UTC().Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors: []cleanAppWireError{{
				Code:    errCode,
				Message: "failed to ingest normalized report",
			}},
		}, httpStatus
	}

	now := time.Now().UTC()
	receiptID := newReceiptID()
	status := laneToStatus(lane)
	nextCheckAfter := sql.NullTime{Time: now.Add(2 * time.Minute), Valid: true}

	submissionRecord := &database.WireSubmissionRaw{
		SubmissionID:      sub.SubmissionID,
		ReceiptID:         receiptID,
		FetcherID:         auth.FetcherID,
		KeyID:             sql.NullString{String: auth.KeyID, Valid: auth.KeyID != ""},
		ActorKind:         nonEmpty(auth.ActorKind, "machine"),
		Channel:           nonEmpty(auth.Channel, "wire"),
		AuthMethod:        nonEmpty(auth.AuthMethod, sub.Agent.AuthMethod),
		SourceID:          sub.SourceID,
		SchemaVersion:     sub.SchemaVersion,
		SubmittedAt:       mustRFC3339(sub.SubmittedAt),
		ObservedAt:        nullableTimeValue(sub.ObservedAt),
		AgentID:           sub.Agent.AgentID,
		Lane:              lane,
		MaterialHash:      materialHash,
		SubmissionQuality: quality,
		RiskScore:         auth.RiskScore,
		AgentJSON:         h.db.MarshalJSON(sub.Agent),
		ProvenanceJSON:    h.db.MarshalJSON(sub.Provenance),
		ReportJSON:        h.db.MarshalJSON(sub.Report),
		DedupeJSON:        h.db.MarshalJSON(sub.Dedupe),
		DeliveryJSON:      h.db.MarshalJSON(sub.Delivery),
		ExtensionsJSON:    h.db.MarshalJSON(sub.Extensions),
	}
	if ingestResp.ReportSeq > 0 {
		submissionRecord.ReportSeq = sql.NullInt64{Int64: int64(ingestResp.ReportSeq), Valid: true}
	}

	receiptRecord := &database.WireReceipt{
		ReceiptID:         receiptID,
		SubmissionID:      sub.SubmissionID,
		FetcherID:         auth.FetcherID,
		SourceID:          sub.SourceID,
		Status:            status,
		Lane:              lane,
		IdempotencyReplay: ingestResp.Status == "duplicate",
		WarningsJSON:      h.db.MarshalJSON(cleanAppWireWarningsForSubmission(sub, lane)),
		NextCheckAfter:    nextCheckAfter,
	}
	if ingestResp.ReportSeq > 0 {
		receiptRecord.ReportSeq = sql.NullInt64{Int64: int64(ingestResp.ReportSeq), Valid: true}
	}

	if err := h.db.InsertWireSubmissionAndReceipt(ctx, submissionRecord, receiptRecord); err != nil {
		return cleanAppWireReceiptResponse{
			ReceiptID:         receiptID,
			SubmissionID:      sub.SubmissionID,
			SourceID:          sub.SourceID,
			ReceivedAt:        now.Format(time.RFC3339),
			Status:            "rejected",
			Lane:              wireLaneReject,
			IdempotencyReplay: false,
			Errors: []cleanAppWireError{{
				Code:    "PERSISTENCE_FAILED",
				Message: "failed to persist submission receipt",
			}},
		}, http.StatusInternalServerError
	}
	_ = h.db.EnsureWireReputationProfile(ctx, auth.FetcherID)
	_ = h.db.IncrementWireReputationSample(ctx, auth.FetcherID)
	if auditEndpoint != "" {
		_ = h.db.InsertIngestionAuditV1(ctx, auth.FetcherID, auth.KeyID, auditEndpoint, 1, 1, 0, nil, 0, clientIP, userAgent, requestID)
	}

	return cleanAppWireReceiptResponse{
		ReceiptID:         receiptID,
		SubmissionID:      sub.SubmissionID,
		SourceID:          sub.SourceID,
		ReceivedAt:        now.Format(time.RFC3339),
		Status:            status,
		Lane:              lane,
		ReportID:          ingestResp.ReportSeq,
		IdempotencyReplay: ingestResp.Status == "duplicate",
		Warnings:          cleanAppWireWarningsForSubmission(sub, lane),
		NextCheckAfter:    nextCheckAfter.Time.UTC().Format(time.RFC3339),
		SubmissionQuality: quality,
	}, http.StatusOK
}

func cleanAppWireIngestCoreItemFromSubmission(sub cleanAppWireSubmission) cleanAppWireIngestCoreItem {
	item := cleanAppWireIngestCoreItem{
		SourceID:     sub.SourceID,
		Title:        sub.Report.Title,
		Description:  cleanAppWireDescription(sub),
		CollectedAt:  chooseObservedAt(sub),
		AgentID:      sub.Agent.AgentID,
		AgentVersion: sub.Agent.SoftwareVersion,
		SourceType:   cleanAppWireSourceType(sub),
		ImageBase64:  cleanAppWireInlineImageBase64(sub),
		Tags:         append([]string(nil), sub.Report.Tags...),
	}
	if sub.Report.Location != nil {
		item.Lat = &sub.Report.Location.Lat
		item.Lng = &sub.Report.Location.Lng
	}
	for _, ev := range sub.Report.EvidenceBundle {
		item.Media = append(item.Media, cleanAppWireIngestCoreMedia{
			URL:         ev.URI,
			SHA256:      ev.SHA256,
			ContentType: ev.MIMEType,
		})
	}
	return item
}

func (h *Handlers) cleanAppWireReceiptRecordToResponse(receipt *database.WireReceipt, quality float64) cleanAppWireReceiptResponse {
	resp := cleanAppWireReceiptResponse{
		ReceiptID:         receipt.ReceiptID,
		SubmissionID:      receipt.SubmissionID,
		SourceID:          receipt.SourceID,
		ReceivedAt:        receipt.CreatedAt.UTC().Format(time.RFC3339),
		Status:            receipt.Status,
		Lane:              receipt.Lane,
		IdempotencyReplay: receipt.IdempotencyReplay,
		SubmissionQuality: quality,
	}
	if receipt.ReportSeq.Valid {
		resp.ReportID = int(receipt.ReportSeq.Int64)
	}
	if len(receipt.WarningsJSON) > 0 {
		var warnings []string
		_ = json.Unmarshal(receipt.WarningsJSON, &warnings)
		resp.Warnings = warnings
	}
	if receipt.NextCheckAfter.Valid {
		resp.NextCheckAfter = receipt.NextCheckAfter.Time.UTC().Format(time.RFC3339)
	}
	if receipt.RejectionCode.Valid {
		resp.Errors = []cleanAppWireError{{
			Code:    receipt.RejectionCode.String,
			Message: receipt.RejectionCode.String,
		}}
	}
	return resp
}

func (h *Handlers) cleanAppWireAuthContext(c *gin.Context) (cleanAppWireAuthContext, bool) {
	fetcherIDAny, _ := c.Get(middlewareCtxKeyFetcherID())
	keyIDAny, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	perMinAny, _ := c.Get(middlewareCtxKeyFetcherMinuteCap())
	dailyAny, _ := c.Get(middlewareCtxKeyFetcherDailyCap())
	tierAny, _ := c.Get("fetcher_tier")
	statusAny, _ := c.Get("fetcher_status")

	fetcherID, _ := fetcherIDAny.(string)
	keyID, _ := keyIDAny.(string)
	perMin, _ := perMinAny.(int)
	daily, _ := dailyAny.(int)
	tier, _ := tierAny.(int)
	status, _ := statusAny.(string)
	if fetcherID == "" || keyID == "" {
		return cleanAppWireAuthContext{}, false
	}
	return cleanAppWireAuthContext{
		FetcherID:   fetcherID,
		KeyID:       keyID,
		PerMinute:   perMin,
		Daily:       daily,
		Tier:        tier,
		Status:      status,
		ActorKind:   "machine",
		Channel:     "wire",
		AuthMethod:  "api_key",
		DisplayName: fetcherID,
	}, true
}

func normalizeCleanAppWireSubmission(sub cleanAppWireSubmission) (cleanAppWireSubmission, string) {
	if strings.TrimSpace(sub.SchemaVersion) == "" {
		sub.SchemaVersion = cleanAppWireSchemaV1
	}
	if strings.TrimSpace(sub.SubmissionID) == "" {
		sub.SubmissionID = "subm_" + strings.ReplaceAll(newReceiptID(), "rcpt_", "")
	}
	if strings.TrimSpace(sub.SubmittedAt) == "" {
		sub.SubmittedAt = time.Now().UTC().Format(time.RFC3339)
	}
	sub.SourceID = clampStr(strings.TrimSpace(sub.SourceID), 255)
	sub.Agent.AgentID = clampStr(strings.TrimSpace(sub.Agent.AgentID), 128)
	sub.Agent.AgentType = normalizeWireSlug(sub.Agent.AgentType)
	sub.Agent.OperatorType = normalizeWireSlug(sub.Agent.OperatorType)
	sub.Agent.AuthMethod = normalizeWireSlug(sub.Agent.AuthMethod)
	sub.Report.Domain = normalizeWireSlug(sub.Report.Domain)
	sub.Report.ProblemType = normalizeWireSlug(sub.Report.ProblemType)
	sub.Report.ProblemSubtype = normalizeWireSlug(sub.Report.ProblemSubtype)
	sub.Report.Title = clampStr(strings.TrimSpace(sub.Report.Title), 255)
	sub.Report.Description = strings.TrimSpace(sub.Report.Description)
	if sub.Report.Location != nil {
		sub.Report.Location.Kind = normalizeWireSlug(sub.Report.Location.Kind)
	}
	if sub.Delivery.RequestedLane == "" {
		sub.Delivery.RequestedLane = "auto"
	}
	return sub, sub.SubmissionID
}

func validateCleanAppWireSubmission(sub cleanAppWireSubmission, strictSignature bool) []cleanAppWireError {
	var errs []cleanAppWireError
	if sub.SchemaVersion != cleanAppWireSchemaV1 {
		errs = append(errs, cleanAppWireError{Code: "INVALID_SCHEMA_VERSION", Message: "schema_version must be cleanapp-wire.v1"})
	}
	if strings.TrimSpace(sub.SourceID) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "source_id is required"})
	}
	if strings.TrimSpace(sub.SubmittedAt) == "" || parseRFC3339(sub.SubmittedAt) == nil {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "submitted_at must be RFC3339"})
	}
	if strings.TrimSpace(sub.Agent.AgentID) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "agent.agent_id is required"})
	}
	if strings.TrimSpace(sub.Agent.AgentType) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "agent.agent_type is required"})
	}
	if strings.TrimSpace(sub.Agent.AuthMethod) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "agent.auth_method is required"})
	}
	if strings.TrimSpace(sub.Report.Domain) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "report.domain is required"})
	}
	if strings.TrimSpace(sub.Report.ProblemType) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "report.problem_type is required"})
	}
	if strings.TrimSpace(sub.Report.Title) == "" {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "report.title is required"})
	}
	if strings.TrimSpace(sub.Report.Description) == "" && len(sub.Report.EvidenceBundle) == 0 {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "report.description or report.evidence_bundle is required"})
	}
	if sub.Report.Location == nil && sub.Report.DigitalContext == nil {
		errs = append(errs, cleanAppWireError{Code: "MISSING_REQUIRED_FIELD", Message: "report.location or report.digital_context is required"})
	}
	if sub.Report.Confidence < 0 || sub.Report.Confidence > 1 {
		errs = append(errs, cleanAppWireError{Code: "CONFIDENCE_OUT_OF_RANGE", Message: "report.confidence must be within [0,1]"})
	}
	if strictSignature && len(strings.TrimSpace(sub.Agent.Signature)) < 12 {
		errs = append(errs, cleanAppWireError{Code: "AUTH_SIGNATURE_INVALID", Message: "signature is required when strict signature enforcement is enabled"})
	}
	return errs
}

func computeCleanAppWireSubmissionQuality(sub cleanAppWireSubmission) float64 {
	evidenceCompleteness := 0.0
	if len(sub.Report.EvidenceBundle) > 0 {
		evidenceCompleteness = minFloat(1, float64(len(sub.Report.EvidenceBundle))/3.0)
	}
	placeCertainty := 0.0
	if sub.Report.Location != nil {
		if sub.Report.Location.PlaceConfidence > 0 {
			placeCertainty = clamp01(sub.Report.Location.PlaceConfidence)
		} else {
			placeCertainty = 0.65
		}
	}
	targetCertainty := 0.0
	if strings.TrimSpace(sub.Report.TargetEntity.Name) != "" {
		targetCertainty = 0.7
	}
	novelty := clamp01(sub.Report.Economic.EstimatedNovelty)
	if novelty == 0 {
		novelty = 0.45
	}
	categoryFit := 0.0
	if strings.TrimSpace(sub.Report.ProblemType) != "" {
		categoryFit = 1
	}
	policyRisk := 0.0
	if sub.Report.Privacy.ContainsPII {
		policyRisk += 0.4
	}
	if sub.Report.Privacy.RequiresRedaction {
		policyRisk += 0.3
	}
	anomaly := 0.0
	if strings.TrimSpace(sub.Dedupe.Fingerprint) == "" && len(sub.Dedupe.NearDuplicateKeys) == 0 {
		anomaly = 0.1
	}
	q := 0.20*clamp01(sub.Report.Confidence) +
		0.20*evidenceCompleteness +
		0.15*placeCertainty +
		0.15*targetCertainty +
		0.15*novelty +
		0.10*categoryFit -
		0.05*clamp01(policyRisk) -
		0.10*clamp01(anomaly)
	return round2(clamp01(q))
}

func assignCleanAppWireLane(cfg *config.Config, tier int, quality float64, evidenceCount int, requestedLane string) string {
	requestedLane = normalizeWireSlug(requestedLane)
	if requestedLane == wireLaneHumanAuto {
		switch {
		case tier < cfg.CleanAppWirePublishLaneMinTier:
			return wireLaneShadow
		case evidenceCount > 0 && quality >= 0.50:
			return wireLanePublish
		default:
			return wireLaneShadow
		}
	}
	if cfg.CleanAppWirePriorityLaneEnabled && requestedLane == wireLanePriority && tier >= 3 && quality >= 0.85 {
		return wireLanePriority
	}
	switch {
	case tier <= 0:
		return wireLaneQuarantine
	case tier < cfg.CleanAppWirePublishLaneMinTier:
		return wireLaneShadow
	case quality >= 0.65 && evidenceCount > 0:
		return wireLanePublish
	default:
		return wireLaneShadow
	}
}

func laneToVisibility(lane string) string {
	switch lane {
	case wireLanePublish, wireLanePriority:
		return "public"
	default:
		return "shadow"
	}
}

func laneToTrustLevel(lane string) string {
	switch lane {
	case wireLanePublish, wireLanePriority:
		return "verified"
	default:
		return "unverified"
	}
}

func laneToStatus(lane string) string {
	switch lane {
	case wireLaneQuarantine:
		return "quarantined"
	case wireLaneShadow:
		return "shadowed"
	case wireLanePublish, wireLanePriority:
		return "published"
	default:
		return "accepted"
	}
}

func cleanAppWireDescription(sub cleanAppWireSubmission) string {
	if strings.TrimSpace(sub.Report.Description) != "" {
		return sub.Report.Description
	}
	if len(sub.Report.EvidenceBundle) > 0 {
		return sub.Report.Title
	}
	return sub.Report.Title
}

func chooseObservedAt(sub cleanAppWireSubmission) string {
	if parseRFC3339(sub.ObservedAt) != nil {
		return sub.ObservedAt
	}
	return sub.SubmittedAt
}

func cleanAppWireSourceType(sub cleanAppWireSubmission) string {
	switch sub.Report.Domain {
	case "physical":
		return "sensor"
	case "digital":
		return "web"
	default:
		return "text"
	}
}

func cleanAppWireInlineImageBase64(sub cleanAppWireSubmission) string {
	if sub.Extensions == nil {
		return ""
	}
	for _, key := range []string{"image_base64", "inline_image_b64"} {
		if raw, ok := sub.Extensions[key]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func decodeWireInlineBytes(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if idx := strings.Index(raw, ","); idx >= 0 && strings.Contains(raw[:idx], ";base64") {
		raw = raw[idx+1:]
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(raw)
}

func cleanAppWireWarningsForSubmission(sub cleanAppWireSubmission, lane string) []string {
	var warnings []string
	if lane == wireLaneQuarantine || lane == wireLaneShadow {
		warnings = append(warnings, "quarantine_first_lane")
	}
	if len(sub.Report.EvidenceBundle) == 0 {
		warnings = append(warnings, "no_evidence_bundle")
	}
	if sub.Report.Location != nil && sub.Report.Location.PlaceConfidence > 0 && sub.Report.Location.PlaceConfidence < 0.5 {
		warnings = append(warnings, "low_place_confidence")
	}
	return warnings
}

func cleanAppWireMaterialHash(sub cleanAppWireSubmission) (string, error) {
	stable := struct {
		SchemaVersion string         `json:"schema_version"`
		SourceID      string         `json:"source_id"`
		ObservedAt    string         `json:"observed_at,omitempty"`
		Agent         any            `json:"agent"`
		Provenance    any            `json:"provenance,omitempty"`
		Report        any            `json:"report"`
		Dedupe        any            `json:"dedupe,omitempty"`
		Delivery      any            `json:"delivery,omitempty"`
		Extensions    map[string]any `json:"extensions,omitempty"`
	}{
		SchemaVersion: sub.SchemaVersion,
		SourceID:      sub.SourceID,
		ObservedAt:    sub.ObservedAt,
		Agent: struct {
			AgentID         string `json:"agent_id"`
			AgentName       string `json:"agent_name,omitempty"`
			AgentType       string `json:"agent_type"`
			OperatorType    string `json:"operator_type,omitempty"`
			AuthMethod      string `json:"auth_method"`
			KeyID           string `json:"key_id,omitempty"`
			SoftwareVersion string `json:"software_version,omitempty"`
			ExecutionMode   string `json:"execution_mode,omitempty"`
		}{
			AgentID:         sub.Agent.AgentID,
			AgentName:       sub.Agent.AgentName,
			AgentType:       sub.Agent.AgentType,
			OperatorType:    sub.Agent.OperatorType,
			AuthMethod:      sub.Agent.AuthMethod,
			KeyID:           sub.Agent.KeyID,
			SoftwareVersion: sub.Agent.SoftwareVersion,
			ExecutionMode:   sub.Agent.ExecutionMode,
		},
		Provenance: sub.Provenance,
		Report:     sub.Report,
		Dedupe:     sub.Dedupe,
		Delivery:   sub.Delivery,
		Extensions: sub.Extensions,
	}
	b, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeWireSlug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func newReceiptID() string {
	id, err := uuidV4()
	if err != nil {
		return fmt.Sprintf("rcpt_%d", time.Now().UnixNano())
	}
	return "rcpt_" + id
}

func nullableTimeValue(s string) sql.NullTime {
	if t := parseRFC3339(s); t != nil {
		return sql.NullTime{Time: *t, Valid: true}
	}
	return sql.NullTime{}
}

func mustRFC3339(s string) time.Time {
	if t := parseRFC3339(s); t != nil {
		return *t
	}
	return time.Now().UTC()
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func nonEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func cleanAppWireReporterID(auth cleanAppWireAuthContext, item cleanAppWireIngestCoreItem) string {
	if auth.ActorKind == "human" {
		if agentID := strings.TrimSpace(item.AgentID); agentID != "" {
			return agentID
		}
	}
	return "fetcher_v1:" + auth.FetcherID
}

func (h *Handlers) cleanAppWireIngestCore(ctx context.Context, auth cleanAppWireAuthContext, item cleanAppWireIngestCoreItem, visibility, trustLevel string) (cleanAppWireIngestCoreResult, string, int) {
	existing, err := h.db.GetExistingReportSeqsV1(ctx, auth.FetcherID, []string{item.SourceID})
	if err != nil {
		return cleanAppWireIngestCoreResult{}, "IDEMPOTENCY_LOOKUP_FAILED", http.StatusInternalServerError
	}

	if seq, dup := existing[item.SourceID]; dup {
		return cleanAppWireIngestCoreResult{
			SourceID:   item.SourceID,
			Status:     "duplicate",
			ReportSeq:  seq,
			Queued:     true,
			Visibility: visibility,
			TrustLevel: trustLevel,
		}, "", http.StatusOK
	}

	title := clampStr(item.Title, 255)
	description := clampStr(item.Description, 8192)
	if description == "" {
		description = title
	}
	lat := 0.0
	lng := 0.0
	if item.Lat != nil {
		lat = *item.Lat
	}
	if item.Lng != nil {
		lng = *item.Lng
	}

	reporterID := cleanAppWireReporterID(auth, item)
	img := []byte{}
	if strings.TrimSpace(item.ImageBase64) != "" {
		decoded, err := decodeWireInlineBytes(item.ImageBase64)
		if err != nil {
			return cleanAppWireIngestCoreResult{}, "INVALID_INLINE_IMAGE", http.StatusBadRequest
		}
		img = decoded
	}

	tx, err := h.db.DB().BeginTx(ctx, nil)
	if err != nil {
		return cleanAppWireIngestCoreResult{}, "BEGIN_TX_FAILED", http.StatusInternalServerError
	}
	defer tx.Rollback()

	reportPublicID, err := publicid.NewReportID()
	if err != nil {
		return cleanAppWireIngestCoreResult{}, "REPORT_PUBLIC_ID_GENERATION_FAILED", http.StatusInternalServerError
	}

	res, err := tx.ExecContext(ctx,
		"INSERT INTO reports (public_id, id, team, action_id, latitude, longitude, x, y, image, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		reportPublicID,
		reporterID,
		0,
		"",
		lat,
		lng,
		0.0,
		0.0,
		img,
		title,
	)
	if err != nil {
		return cleanAppWireIngestCoreResult{}, "REPORT_INSERT_FAILED", http.StatusInternalServerError
	}
	firstID, _ := res.LastInsertId()
	seq := int(firstID)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO report_raw (
			report_seq, fetcher_id, source_id, agent_id, agent_version, collected_at, source_type,
			visibility, trust_level, promoted_to_public_at, spam_score
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		seq,
		auth.FetcherID,
		item.SourceID,
		nullable(item.AgentID),
		nullable(item.AgentVersion),
		nullableTime(parseRFC3339(item.CollectedAt)),
		nullable(item.SourceType),
		visibility,
		trustLevel,
		func() interface{} {
			if visibility == "public" {
				return time.Now().UTC()
			}
			return nil
		}(),
	)
	if err != nil {
		return cleanAppWireIngestCoreResult{}, "REPORT_RAW_INSERT_FAILED", http.StatusInternalServerError
	}

	if err := tx.Commit(); err != nil {
		return cleanAppWireIngestCoreResult{}, "COMMIT_FAILED", http.StatusInternalServerError
	}

	pub, err := h.ensureRabbitMQPublisher()
	if err != nil {
		return cleanAppWireIngestCoreResult{
			SourceID:   item.SourceID,
			Status:     "accepted",
			ReportSeq:  seq,
			Queued:     false,
			Visibility: visibility,
			TrustLevel: trustLevel,
		}, "QUEUE_UNAVAILABLE", http.StatusServiceUnavailable
	}

	msg := map[string]interface{}{
		"seq":         seq,
		"description": description,
		"latitude":    lat,
		"longitude":   lng,
		"fetcher_id":  auth.FetcherID,
		"source_id":   item.SourceID,
		"visibility":  visibility,
		"tags":        item.Tags,
	}
	if err := pub.PublishWithRoutingKey(h.cfg.RabbitRawReportRoutingKey, msg); err != nil {
		return cleanAppWireIngestCoreResult{
			SourceID:   item.SourceID,
			Status:     "accepted",
			ReportSeq:  seq,
			Queued:     false,
			Visibility: visibility,
			TrustLevel: trustLevel,
		}, "QUEUE_PUBLISH_FAILED", http.StatusServiceUnavailable
	}

	return cleanAppWireIngestCoreResult{
		SourceID:   item.SourceID,
		Status:     "accepted",
		ReportSeq:  seq,
		Queued:     true,
		Visibility: visibility,
		TrustLevel: trustLevel,
	}, "", http.StatusOK
}

func cleanAppWireIngestCoreResultToV1ItemResult(res cleanAppWireIngestCoreResult) v1BulkIngestItemResult {
	return v1BulkIngestItemResult{
		SourceID:   res.SourceID,
		Status:     res.Status,
		ReportSeq:  res.ReportSeq,
		Queued:     res.Queued,
		Visibility: res.Visibility,
		TrustLevel: res.TrustLevel,
	}
}

func v1BulkIngestItemToCleanAppWireIngestCoreItem(it v1BulkIngestItem) cleanAppWireIngestCoreItem {
	item := cleanAppWireIngestCoreItem{
		SourceID:     it.SourceID,
		Title:        it.Title,
		Description:  it.Description,
		Lat:          it.Lat,
		Lng:          it.Lng,
		CollectedAt:  it.CollectedAt,
		AgentID:      it.AgentID,
		AgentVersion: it.AgentVersion,
		SourceType:   it.SourceType,
		ImageBase64:  it.ImageBase64,
		Tags:         append([]string(nil), it.Tags...),
	}
	for _, media := range it.Media {
		item.Media = append(item.Media, cleanAppWireIngestCoreMedia{
			URL:         media.URL,
			SHA256:      media.SHA256,
			ContentType: media.ContentType,
		})
	}
	return item
}

func (h *Handlers) cleanAppWireIngestSingleViaV1(ctx context.Context, auth cleanAppWireAuthContext, req v1BulkIngestRequest, visibility, trustLevel string) (v1BulkIngestItemResult, string, int) {
	if len(req.Items) != 1 {
		return v1BulkIngestItemResult{}, "INVALID_BATCH", http.StatusBadRequest
	}
	coreResp, errCode, httpStatus := h.cleanAppWireIngestCore(ctx, auth, v1BulkIngestItemToCleanAppWireIngestCoreItem(req.Items[0]), visibility, trustLevel)
	return cleanAppWireIngestCoreResultToV1ItemResult(coreResp), errCode, httpStatus
}
