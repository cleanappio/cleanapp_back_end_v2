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

	"github.com/gin-gonic/gin"
)

const (
	humanChannelMobile   = "mobile"
	humanChannelWeb      = "web"
	humanChannelLegacyV2 = "legacy_v2"
)

type humanReportSubmissionRequest struct {
	Version    string  `json:"version"`
	Channel    string  `json:"channel"`
	AppVersion string  `json:"app_version,omitempty"`
	SessionID  string  `json:"session_id,omitempty"`
	DeviceID   string  `json:"device_id,omitempty"`
	ReporterID string  `json:"reporter_id,omitempty"`
	SourceID   string  `json:"source_id,omitempty"`
	ObservedAt string  `json:"observed_at,omitempty"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Image      []byte  `json:"image,omitempty"`
	ActionID   string  `json:"action_id,omitempty"`
	Annotation string  `json:"annotation,omitempty"`
}

type humanReportReceiptResponse struct {
	cleanAppWireReceiptResponse
	PublicID      string `json:"public_id,omitempty"`
	CanonicalPath string `json:"canonical_path,omitempty"`
}

func normalizeHumanChannel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case humanChannelWeb:
		return humanChannelWeb
	case humanChannelLegacyV2, "legacy", "v2":
		return humanChannelLegacyV2
	default:
		return humanChannelMobile
	}
}

func syntheticHumanFetcherID(channel string) string {
	return "human-" + normalizeHumanChannel(channel)
}

func syntheticHumanSourceID(prefix string, req humanReportSubmissionRequest) string {
	if trimmed := strings.TrimSpace(req.SourceID); trimmed != "" {
		return trimmed
	}
	fingerprint := struct {
		Channel    string  `json:"channel"`
		ReporterID string  `json:"reporter_id,omitempty"`
		DeviceID   string  `json:"device_id,omitempty"`
		SessionID  string  `json:"session_id,omitempty"`
		ObservedAt string  `json:"observed_at,omitempty"`
		Latitude   float64 `json:"latitude"`
		Longitude  float64 `json:"longitude"`
		X          float64 `json:"x,omitempty"`
		Y          float64 `json:"y,omitempty"`
		ActionID   string  `json:"action_id,omitempty"`
		Annotation string  `json:"annotation,omitempty"`
		ImageHash  string  `json:"image_hash,omitempty"`
	}{
		Channel:    normalizeHumanChannel(req.Channel),
		ReporterID: strings.TrimSpace(req.ReporterID),
		DeviceID:   strings.TrimSpace(req.DeviceID),
		SessionID:  strings.TrimSpace(req.SessionID),
		ObservedAt: strings.TrimSpace(req.ObservedAt),
		Latitude:   req.Latitude,
		Longitude:  req.Longitude,
		X:          req.X,
		Y:          req.Y,
		ActionID:   strings.TrimSpace(req.ActionID),
		Annotation: strings.TrimSpace(req.Annotation),
	}
	if len(req.Image) > 0 {
		sum := sha256.Sum256(req.Image)
		fingerprint.ImageHash = hex.EncodeToString(sum[:])
	}
	payload, _ := json.Marshal(fingerprint)
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s_%s_%s", prefix, normalizeHumanChannel(req.Channel), hex.EncodeToString(sum[:12]))
}

func humanAuthMethod(req humanReportSubmissionRequest) string {
	if strings.TrimSpace(req.ReporterID) != "" {
		return "wallet_address"
	}
	if strings.TrimSpace(req.SessionID) != "" || strings.TrimSpace(req.DeviceID) != "" {
		return "device_session"
	}
	return "anonymous_client"
}

func humanAgentID(req humanReportSubmissionRequest) string {
	if trimmed := strings.TrimSpace(req.ReporterID); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(req.DeviceID); trimmed != "" {
		return trimmed
	}
	return syntheticHumanFetcherID(req.Channel)
}

func (h *Handlers) humanReportAuthContext(channel string) cleanAppWireAuthContext {
	tier := h.cfg.CleanAppWirePublishLaneMinTier
	if tier < 2 {
		tier = 2
	}
	return cleanAppWireAuthContext{
		FetcherID:   syntheticHumanFetcherID(channel),
		Tier:        tier,
		Status:      "active",
		ActorKind:   "human",
		Channel:     normalizeHumanChannel(channel),
		AuthMethod:  "device_session",
		RiskScore:   0.25,
		DisplayName: "human-submission",
	}
}

func humanReportTitle(annotation string) string {
	title := strings.TrimSpace(annotation)
	if title == "" {
		return "Human report submission"
	}
	if len(title) > 255 {
		return title[:255]
	}
	return title
}

func buildHumanWireSubmission(req humanReportSubmissionRequest, sourcePrefix string) cleanAppWireSubmission {
	channel := normalizeHumanChannel(req.Channel)
	submittedAt := time.Now().UTC().Format(time.RFC3339)
	observedAt := strings.TrimSpace(req.ObservedAt)
	if observedAt == "" || parseRFC3339(observedAt) == nil {
		observedAt = submittedAt
	}

	title := humanReportTitle(req.Annotation)
	description := strings.TrimSpace(req.Annotation)
	if description == "" {
		description = title
	}

	sourceID := syntheticHumanSourceID(sourcePrefix, req)
	sub := cleanAppWireSubmission{
		SchemaVersion: cleanAppWireSchemaV1,
		SourceID:      sourceID,
		SubmittedAt:   submittedAt,
		ObservedAt:    observedAt,
		Extensions: map[string]any{
			"action_id":  req.ActionID,
			"x":          req.X,
			"y":          req.Y,
			"device_id":  strings.TrimSpace(req.DeviceID),
			"session_id": strings.TrimSpace(req.SessionID),
		},
	}
	sub.Agent.AgentID = humanAgentID(req)
	sub.Agent.AgentName = channel
	sub.Agent.AgentType = "human_client"
	sub.Agent.OperatorType = "human"
	sub.Agent.AuthMethod = humanAuthMethod(req)
	sub.Agent.SoftwareVersion = strings.TrimSpace(req.AppVersion)
	sub.Agent.ExecutionMode = channel
	sub.Provenance.GenerationMethod = "human_submission"
	sub.Provenance.ChainOfCustody = []string{"human_submission", channel}
	sub.Provenance.HumanInLoop = true
	// Human mobile/web submissions should be evaluated with the human-specific lane
	// policy so image-backed reports are publishable without needing machine-target metadata.
	sub.Delivery.RequestedLane = "human_auto"
	sub.Report.Domain = "physical"
	sub.Report.ProblemType = "citizen_submission"
	sub.Report.Title = title
	sub.Report.Description = description
	sub.Report.Language = "en"
	if len(req.Image) > 0 {
		sub.Report.Confidence = 0.9
	} else {
		sub.Report.Confidence = 0.7
	}
	sub.Report.Location = &struct {
		Kind            string  `json:"kind,omitempty"`
		Lat             float64 `json:"lat"`
		Lng             float64 `json:"lng"`
		Geohash         string  `json:"geohash,omitempty"`
		AddressText     string  `json:"address_text,omitempty"`
		PlaceConfidence float64 `json:"place_confidence,omitempty"`
	}{
		Kind:            "coordinate",
		Lat:             req.Latitude,
		Lng:             req.Longitude,
		PlaceConfidence: 1,
	}
	if len(req.Image) > 0 {
		encoded := base64.StdEncoding.EncodeToString(req.Image)
		sub.Extensions["image_base64"] = encoded
		sum := sha256.Sum256(req.Image)
		sub.Report.EvidenceBundle = []struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			{
				EvidenceID: "inline-image",
				Type:       "inline_image",
				SHA256:     hex.EncodeToString(sum[:]),
				MIMEType:   "application/octet-stream",
				CapturedAt: observedAt,
			},
		}
	}
	return sub
}

func (h *Handlers) humanReceiptResponseFromWire(
	ctx context.Context,
	receipt cleanAppWireReceiptResponse,
) humanReportReceiptResponse {
	resp := humanReportReceiptResponse{
		cleanAppWireReceiptResponse: receipt,
	}
	if receipt.ReportID <= 0 {
		return resp
	}
	report, err := h.db.GetReportBySeq(ctx, receipt.ReportID)
	if err != nil || report == nil {
		return resp
	}
	resp.PublicID = strings.TrimSpace(report.Report.PublicID)
	resp.CanonicalPath = canonicalReportPath("physical", resp.PublicID)
	return resp
}

func (h *Handlers) SubmitHumanReportV1(c *gin.Context) {
	if !h.cfg.HumanIngestEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "human ingest disabled"})
		return
	}

	var req humanReportSubmissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.Version != "" && req.Version != "1.0" && req.Version != "2.0" {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "unsupported version"})
		return
	}
	if req.Latitude < -90 || req.Latitude > 90 || req.Longitude < -180 || req.Longitude > 180 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid coordinates"})
		return
	}

	channel := normalizeHumanChannel(req.Channel)
	auth := h.humanReportAuthContext(channel)
	auth.AuthMethod = humanAuthMethod(req)
	auth.DisplayName = channel
	if err := h.db.EnsureFetcherV1Upsert(
		c.Request.Context(),
		auth.FetcherID,
		"Human "+strings.ToUpper(channel),
		"human",
		"active",
		auth.Tier,
		"public",
		"community",
		false,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare human ingest profile"})
		return
	}

	submission := buildHumanWireSubmission(req, h.cfg.HumanIngestReportSourcePrefix)
	receipt, statusCode := h.processCleanAppWireSubmissionInternal(
		c.Request.Context(),
		auth,
		submission,
		c.ClientIP(),
		c.GetHeader("User-Agent"),
		c.GetHeader("X-Request-Id"),
		"/api/v1/human-reports/submit",
	)
	c.JSON(statusCode, h.humanReceiptResponseFromWire(c.Request.Context(), receipt))
}

func (h *Handlers) GetHumanReceiptV1(c *gin.Context) {
	if !h.cfg.HumanIngestEnabled || !h.cfg.HumanIngestReceiptLookupEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "human receipt lookup disabled"})
		return
	}
	receiptID := strings.TrimSpace(c.Param("receipt_id"))
	if receiptID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "receipt_id is required"})
		return
	}
	receipt, err := h.db.GetWireReceiptByID(c.Request.Context(), receiptID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "receipt not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load receipt"})
		return
	}
	if !strings.HasPrefix(receipt.FetcherID, "human-") {
		c.JSON(http.StatusForbidden, gin.H{"error": "receipt is not available on the public human ingest surface"})
		return
	}
	c.JSON(http.StatusOK, h.humanReceiptResponseFromWire(
		c.Request.Context(),
		h.cleanAppWireReceiptRecordToResponse(receipt, 0),
	))
}
