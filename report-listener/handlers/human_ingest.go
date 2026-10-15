package handlers

import (
	"bytes"
	"cleanapp-common/edge"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	humanChannelMobile         = "mobile"
	humanChannelWeb            = "web"
	humanChannelShare          = "share"
	humanChannelLegacyV2       = "legacy_v2"
	maxHumanWebAnnotationBytes = 8192
	maxHumanWebImageBytes      = 1024 * 1024
	maxHumanWebImageDimension  = 4096
	maxHumanWebImagePixels     = 16 * 1024 * 1024
)

// One submit limiter shared by all aliases; a client-supplied session is
// provenance, never authentication or a way to obtain a fresh rate bucket.
var humanWebSubmitLimit = edge.RateLimitMiddleware(edge.RateLimitConfig{RPS: 0.05, Burst: 2})

type humanReportSubmissionRequest struct {
	Version    string   `json:"version"`
	Channel    string   `json:"channel"`
	AppVersion string   `json:"app_version,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	DeviceID   string   `json:"device_id,omitempty"`
	ReporterID string   `json:"reporter_id,omitempty"`
	SourceID   string   `json:"source_id,omitempty"`
	ObservedAt string   `json:"observed_at,omitempty"`
	Latitude   *float64 `json:"latitude,omitempty"`
	Longitude  *float64 `json:"longitude,omitempty"`
	X          float64  `json:"x,omitempty"`
	Y          float64  `json:"y,omitempty"`
	Image      []byte   `json:"image,omitempty"`
	ActionID   string   `json:"action_id,omitempty"`
	Annotation string   `json:"annotation,omitempty"`
}

type humanReportReceiptResponse struct {
	cleanAppWireReceiptResponse
	PublicID          string `json:"public_id,omitempty"`
	CanonicalPath     string `json:"canonical_path,omitempty"`
	ProcessingStatus  string `json:"processing_status,omitempty"`
	PublicationStatus string `json:"publication_status,omitempty"`
}

func normalizeHumanChannel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case humanChannelWeb:
		return humanChannelWeb
	case humanChannelShare:
		return humanChannelShare
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
		Channel    string   `json:"channel"`
		ReporterID string   `json:"reporter_id,omitempty"`
		DeviceID   string   `json:"device_id,omitempty"`
		SessionID  string   `json:"session_id,omitempty"`
		ObservedAt string   `json:"observed_at,omitempty"`
		Latitude   *float64 `json:"latitude,omitempty"`
		Longitude  *float64 `json:"longitude,omitempty"`
		X          float64  `json:"x,omitempty"`
		Y          float64  `json:"y,omitempty"`
		ActionID   string   `json:"action_id,omitempty"`
		Annotation string   `json:"annotation,omitempty"`
		ImageHash  string   `json:"image_hash,omitempty"`
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
	if normalizeHumanChannel(req.Channel) == humanChannelWeb {
		return "anonymous_client"
	}
	if strings.TrimSpace(req.ReporterID) != "" {
		return "wallet_address"
	}
	if strings.TrimSpace(req.SessionID) != "" || strings.TrimSpace(req.DeviceID) != "" {
		return "device_session"
	}
	return "anonymous_client"
}

func humanAgentID(req humanReportSubmissionRequest) string {
	if normalizeHumanChannel(req.Channel) == humanChannelWeb {
		return syntheticHumanFetcherID(req.Channel)
	}
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
	return clampStr(title, 255)
}

func validateHumanWebSubmission(req humanReportSubmissionRequest) error {
	if !utf8.ValidString(req.Annotation) || len(req.Annotation) > maxHumanWebAnnotationBytes {
		return fmt.Errorf("text must be valid UTF-8 and at most 8192 bytes")
	}
	if strings.TrimSpace(req.Annotation) == "" && len(req.Image) == 0 {
		return fmt.Errorf("add a description or an image")
	}
	if len(req.Image) == 0 {
		return nil
	}
	if len(req.Image) > maxHumanWebImageBytes {
		return fmt.Errorf("image must be at most 1 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(req.Image))
	if err != nil || (format != "jpeg" && format != "png") {
		return fmt.Errorf("image must be a valid JPEG or PNG")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxHumanWebImageDimension || cfg.Height > maxHumanWebImageDimension || int64(cfg.Width)*int64(cfg.Height) > maxHumanWebImagePixels {
		return fmt.Errorf("image dimensions exceed 4096 pixels or 16 megapixels")
	}
	// A valid header alone does not establish a decodable, complete image.
	if _, _, err := image.Decode(bytes.NewReader(req.Image)); err != nil {
		return fmt.Errorf("image must be a complete JPEG or PNG")
	}
	return nil
}

func buildHumanWireSubmission(req humanReportSubmissionRequest, sourcePrefix string) cleanAppWireSubmission {
	channel := normalizeHumanChannel(req.Channel)
	submittedAt := time.Now().UTC().Format(time.RFC3339)
	observedAt := strings.TrimSpace(req.ObservedAt)
	if parseRFC3339(observedAt) == nil {
		observedAt = ""
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
			"observation_time_supplied": observedAt != "",
			"action_id":                 req.ActionID,
			"x":                         req.X,
			"y":                         req.Y,
			"device_id":                 strings.TrimSpace(req.DeviceID),
			"session_id":                strings.TrimSpace(req.SessionID),
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
	if channel == humanChannelWeb {
		sub.Report.Domain = "unknown"
		sub.Delivery.RequestedLane = wireLaneShadow
		sub.Extensions["web_intake_pending_analysis"] = true
	}
	sub.Report.ProblemType = "citizen_submission"
	sub.Report.Title = title
	sub.Report.Description = description
	sub.Report.Language = "en"
	if len(req.Image) > 0 {
		sub.Report.Confidence = 0.9
	} else {
		sub.Report.Confidence = 0.7
	}
	if channel != humanChannelWeb || (req.Latitude != nil && req.Longitude != nil) {
		lat, lng := 0.0, 0.0
		if req.Latitude != nil {
			lat = *req.Latitude
		}
		if req.Longitude != nil {
			lng = *req.Longitude
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
			Lat:             lat,
			Lng:             lng,
			PlaceConfidence: 1,
		}
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
	var publicID, visibility, classification, fetcherID string
	var analyzed bool
	err := h.db.DB().QueryRowContext(ctx, `SELECT r.public_id, COALESCE(rr.visibility,'shadow'), COALESCE(rr.fetcher_id,''),
		EXISTS(SELECT 1 FROM report_analysis ra WHERE ra.seq=r.seq AND ra.language='en'
			AND (rr.visibility='public' OR (COALESCE(ev.evidence_version,0)>0
			AND (ra.is_valid=FALSE OR ev.report_kind<>'firsthand')))),
		COALESCE((SELECT ra.classification FROM report_analysis ra WHERE ra.seq=r.seq AND ra.language='en' LIMIT 1),'')
		FROM reports r LEFT JOIN report_raw rr ON rr.report_seq=r.seq
		LEFT JOIN report_evidence ev ON ev.seq=r.seq WHERE r.seq=?`, receipt.ReportID).
		Scan(&publicID, &visibility, &fetcherID, &analyzed, &classification)
	if err != nil {
		return resp
	}
	return humanReceiptWithProcessingState(resp, publicID, visibility, fetcherID, analyzed, classification)
}

func humanReceiptWithProcessingState(resp humanReportReceiptResponse, publicID, visibility, fetcherID string, analyzed bool, classification string) humanReportReceiptResponse {
	resp.PublicID = strings.TrimSpace(publicID)
	if fetcherID != "human-web" {
		// Preserve the mobile/legacy receipt contract.
		if visibility == "public" {
			resp.CanonicalPath = canonicalReportPath("physical", resp.PublicID)
		}
		return resp
	}
	resp.ProcessingStatus = "pending_analysis"
	resp.PublicationStatus = "pending_analysis"
	resp.NextCheckAfter = time.Now().UTC().Add(5 * time.Second).Format(time.RFC3339)
	if analyzed {
		resp.NextCheckAfter = ""
		resp.ProcessingStatus = "analyzed"
		resp.PublicationStatus = "review_required"
		if visibility == "public" && (classification == "physical" || classification == "digital") {
			resp.PublicationStatus = "public"
			resp.CanonicalPath = canonicalReportPath(classification, resp.PublicID)
		}
	}
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
	// Browser callers always use the web policy, even if they supply a mobile
	// channel label. Native mobile clients continue to omit Origin as before.
	if strings.TrimSpace(c.GetHeader("Origin")) != "" {
		req.Channel = humanChannelWeb
	}
	if req.Version != "" && req.Version != "1.0" && req.Version != "2.0" {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "unsupported version"})
		return
	}
	if (req.Latitude != nil && (*req.Latitude < -90 || *req.Latitude > 90)) || (req.Longitude != nil && (*req.Longitude < -180 || *req.Longitude > 180)) || ((req.Latitude == nil) != (req.Longitude == nil)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid coordinates"})
		return
	}

	channel := normalizeHumanChannel(req.Channel)
	if channel == humanChannelWeb {
		humanWebSubmitLimit(c)
		if c.IsAborted() {
			return
		}
		if err := validateHumanWebSubmission(req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
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
	if channel != humanChannelWeb && receipt.ReportID > 0 && strings.TrimSpace(req.DeviceID) != "" {
		if err := h.db.LinkReportToPushInstall(c.Request.Context(), receipt.ReportID, req.DeviceID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to link report delivery notifications"})
			return
		}
	}
	if channel != humanChannelWeb && receipt.ReportID > 0 && !receipt.IdempotencyReplay && strings.TrimSpace(req.ReporterID) != "" {
		if err := h.db.IncrementReporterDailyKITNs(c.Request.Context(), req.ReporterID); err != nil {
			log.Printf("warn: failed to increment KITNs for human report %d (%s): %v", receipt.ReportID, req.ReporterID, err)
		}
	}
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
