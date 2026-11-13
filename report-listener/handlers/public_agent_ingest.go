package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"report-listener/database"
	"report-listener/publicdiscovery"

	"github.com/gin-gonic/gin"
)

const (
	publicAgentActor      = "public-agent-anonymous-v1"
	publicAgentChannel    = "public_agent"
	publicAgentPath       = "/api/v1/public/agent-reports"
	publicAgentReceiptTTL = 30 * 24 * time.Hour
)

// This is a capacity bound, not an identity bucket. Durable quotas additionally
// bound new intents across restarts and replicas.
var publicAgentSlots = make(chan struct{}, 8)
var publicAgentSourceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var errPublicReceiptExpired = errors.New("private receipt capability has expired")

type publicAgentReportRequest struct {
	Description string   `json:"description"`
	Title       string   `json:"title,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	AgentName   string   `json:"agent_name,omitempty"`
	URL         string   `json:"url,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
}

type publicAgentReceiptResponse struct {
	ReceiptID         string              `json:"receipt_id"`
	SourceID          string              `json:"source_id,omitempty"`
	Status            string              `json:"status"`
	PublicationStatus string              `json:"publication_status"`
	IdempotencyReplay bool                `json:"idempotency_replay"`
	TrackingToken     string              `json:"tracking_token,omitempty"`
	TrackingExpiresAt string              `json:"tracking_expires_at,omitempty"`
	StatusURL         string              `json:"status_url"`
	ReceivedAt        string              `json:"received_at"`
	ProcessingStatus  string              `json:"processing_status"`
	Message           string              `json:"message"`
	Errors            []cleanAppWireError `json:"errors,omitempty"`
}

func isPublicAgentAuth(auth cleanAppWireAuthContext) bool {
	return auth.FetcherID == publicAgentActor && auth.ActorKind == "machine" && auth.Channel == publicAgentChannel && auth.AuthMethod == "anonymous_public"
}

func wireDefersQueueUntilReceipt(auth cleanAppWireAuthContext) bool {
	return auth.ActorKind == "human" && auth.Channel == humanChannelWeb || isPublicAgentAuth(auth)
}

func (h *Handlers) queueCleanAppWireForReceipt(ctx context.Context, auth cleanAppWireAuthContext, item cleanAppWireIngestCoreItem, seq int, visibility, receiptID string) string {
	if !isPublicAgentAuth(auth) {
		return h.queueCleanAppWireCoreItem(auth, item, seq, visibility)
	}
	if err := h.db.SetWireReceiptQueueState(ctx, auth.FetcherID, receiptID, "QUEUE_PENDING"); err != nil {
		return "QUEUE_STATE_UNAVAILABLE"
	}
	code := h.queueCleanAppWireCoreItem(auth, item, seq, visibility)
	if err := h.db.SetWireReceiptQueueState(ctx, auth.FetcherID, receiptID, code); err != nil {
		return "QUEUE_STATE_UNAVAILABLE"
	}
	return code
}

func validatePublicAgentRequest(req *publicAgentReportRequest) error {
	req.Description = strings.TrimSpace(req.Description)
	req.Title = strings.TrimSpace(req.Title)
	req.SourceID = strings.ToLower(strings.TrimSpace(req.SourceID))
	req.AgentName = strings.TrimSpace(req.AgentName)
	req.URL = strings.TrimSpace(req.URL)
	if !utf8.ValidString(req.Description) || req.Description == "" || len(req.Description) > 8192 {
		return fmt.Errorf("description is required and must be valid UTF-8, at most 8192 bytes")
	}
	if !utf8.ValidString(req.Title) || len(req.Title) > 255 || !utf8.ValidString(req.AgentName) || utf8.RuneCountInString(req.AgentName) > 80 {
		return fmt.Errorf("title must be at most 255 UTF-8 bytes and agent_name at most 80 characters")
	}
	if req.SourceID != "" && !publicAgentSourceIDPattern.MatchString(req.SourceID) {
		return fmt.Errorf("source_id must be a private random UUIDv4; generate it once and reuse it only for retries of this report")
	}
	if (req.Latitude == nil) != (req.Longitude == nil) {
		return fmt.Errorf("latitude and longitude must be supplied together")
	}
	if req.Latitude != nil && (math.IsNaN(*req.Latitude) || math.IsInf(*req.Latitude, 0) || math.IsNaN(*req.Longitude) || math.IsInf(*req.Longitude, 0) || *req.Latitude < -90 || *req.Latitude > 90 || *req.Longitude < -180 || *req.Longitude > 180) {
		return fmt.Errorf("invalid coordinates")
	}
	if req.URL != "" {
		u, err := url.Parse(req.URL)
		if err != nil || len(req.URL) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("url must be an HTTP or HTTPS URL without embedded credentials, at most 2048 bytes")
		}
	}
	return nil
}

func buildPublicAgentWireSubmission(req publicAgentReportRequest) cleanAppWireSubmission {
	sum := sha256.Sum256([]byte(req.SourceID))
	sub := cleanAppWireSubmission{SchemaVersion: cleanAppWireSchemaV1, SourceID: "public-agent:" + hex.EncodeToString(sum[:]), SubmittedAt: time.Now().UTC().Format(time.RFC3339)}
	sub.Agent.AgentID = publicAgentActor
	sub.Agent.AgentName = req.AgentName
	sub.Agent.AgentType = "public_agent"
	sub.Agent.AuthMethod = "anonymous_public"
	sub.Provenance.GenerationMethod = "agent_submission"
	sub.Report.Domain = "unknown"
	sub.Report.ProblemType = "observed_problem"
	sub.Report.Title = req.Title
	if sub.Report.Title == "" {
		sub.Report.Title = clampStr(req.Description, 255)
	}
	sub.Report.Description = req.Description
	sub.Report.Confidence = 0.5
	if req.Latitude != nil {
		sub.Report.Domain = "physical"
		// Reuse the canonical adapter's location structure without inventing a place.
		physical := buildHumanWireSubmission(humanReportSubmissionRequest{Channel: "web", Latitude: req.Latitude, Longitude: req.Longitude, Annotation: req.Description}, "unused")
		sub.Report.Location = physical.Report.Location
	}
	if req.URL != "" {
		if sub.Report.Domain == "unknown" {
			sub.Report.Domain = "digital"
		}
		sub.Report.DigitalContext = map[string]string{"url": req.URL}
	}
	return sub
}

func positivePublicCap(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (h *Handlers) SubmitPublicAgentReportV1(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !h.cfg.PublicAgentIngestEnabled || !h.cfg.CleanAppWireEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "public agent intake is temporarily disabled", "code": "INTAKE_DISABLED"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var req publicAgentReportRequest
	err := decoder.Decode(&req)
	if err == nil {
		var extra any
		tailErr := decoder.Decode(&extra)
		if tailErr != io.EOF {
			if tailErr == nil {
				err = fmt.Errorf("expected one JSON object")
			} else {
				err = tailErr
			}
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request must be at most 16 KiB"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "send one JSON object using documented fields"})
		return
	}
	if err := validatePublicAgentRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SourceID == "" {
		id, err := uuidV4()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cannot create report intent"})
			return
		}
		req.SourceID = id
	}
	select {
	case publicAgentSlots <- struct{}{}:
		defer func() { <-publicAgentSlots }()
	default:
		publicAgentBusy(c, "intake is busy; retry the same source_id")
		return
	}
	sub := buildPublicAgentWireSubmission(req)
	lockHash := sha256.Sum256([]byte(sub.SourceID))
	release, locked, err := h.db.TryPublicAgentIntentLock(c.Request.Context(), "pa:"+hex.EncodeToString(lockHash[:28]))
	if err != nil || !locked {
		publicAgentBusy(c, "intake is busy; retry the same source_id")
		return
	}
	defer release()
	existing, existingErr := h.db.GetWireSubmissionByFetcherAndSource(c.Request.Context(), publicAgentActor, sub.SourceID)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		publicAgentBusy(c, "intake is unavailable; retry the same source_id")
		return
	}
	if existingErr == nil {
		original, err := h.db.GetWireReceipt(c.Request.Context(), publicAgentActor, existing.ReceiptID)
		if err != nil {
			publicAgentBusy(c, "receipt lookup unavailable; retry the same source_id")
			return
		}
		if !time.Now().Before(original.CreatedAt.Add(publicAgentReceiptTTL)) {
			c.JSON(http.StatusGone, gin.H{"error": "private receipt access expired; the original report remains retained", "code": "RECEIPT_EXPIRED"})
			return
		}
	}
	if errors.Is(existingErr, sql.ErrNoRows) {
		mac := hmac.New(sha256.New, []byte(h.cfg.JWTSecret))
		mac.Write([]byte("public-agent-ip-v1\x00" + c.ClientIP()))
		ipBucket := hex.EncodeToString(mac.Sum(nil)[:16])
		budgets := []database.QuotaBudgetV1{
			{FetcherID: publicAgentActor, KeyID: "global", PerMinuteCap: positivePublicCap(h.cfg.PublicAgentGlobalMinuteCap, 30), DailyCap: positivePublicCap(h.cfg.PublicAgentGlobalDailyCap, 1000)},
			{FetcherID: publicAgentActor, KeyID: ipBucket, PerMinuteCap: positivePublicCap(h.cfg.PublicAgentPerIPMinuteCap, 5), DailyCap: positivePublicCap(h.cfg.PublicAgentPerIPDailyCap, 50)},
		}
		if err := h.db.ConsumeQuotaBudgetsV1(c.Request.Context(), budgets, time.Now(), 1); err != nil {
			if errors.Is(err, database.ErrQuotaExceeded) {
				c.Header("Retry-After", "60")
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "public intake quota reached; retry later using the same source_id", "code": "QUOTA_EXCEEDED"})
				return
			}
			publicAgentBusy(c, "intake is unavailable; retry the same source_id")
			return
		}
		if err := h.db.EnsurePublicAgentActor(c.Request.Context(), publicAgentActor); err != nil {
			publicAgentBusy(c, "intake is unavailable; retry the same source_id")
			return
		}
	}
	auth := cleanAppWireAuthContext{FetcherID: publicAgentActor, Status: "active", Tier: 0, ActorKind: "machine", Channel: publicAgentChannel, AuthMethod: "anonymous_public", RiskScore: 0.5}
	receipt, code := h.processCleanAppWireSubmissionInternal(c.Request.Context(), auth, sub, c.ClientIP(), c.GetHeader("User-Agent"), c.GetHeader("X-Request-Id"), publicAgentPath)
	if receipt.Lane != wireLaneQuarantine || receipt.ReportID <= 0 {
		if code < 400 {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": "report was not accepted", "errors": receipt.Errors})
		return
	}
	// Anchor the capability's expiry to the database's original receipt time,
	// including on replay and after a broker failure. Retries never extend it.
	stored, err := h.db.GetWireReceipt(c.Request.Context(), publicAgentActor, receipt.ReceiptID)
	if err != nil {
		publicAgentBusy(c, "receipt is retained; retry the same source_id")
		return
	}
	receipt.ReceivedAt = stored.CreatedAt.UTC().Format(time.RFC3339)
	resp, err := h.publicAgentReceipt(receipt)
	if err != nil {
		if errors.Is(err, errPublicReceiptExpired) {
			c.JSON(http.StatusGone, gin.H{"error": "private receipt access expired; the original report remains retained", "code": "RECEIPT_EXPIRED"})
			return
		}
		publicAgentBusy(c, "receipt is retained; retry the same source_id")
		return
	}
	resp.SourceID = req.SourceID
	if code >= 500 {
		c.Header("Retry-After", "5")
		resp.ProcessingStatus = "pending_retry"
		resp.Message = "Report retained privately. Retry the same source_id and payload to resume processing."
	}
	c.JSON(code, resp)
}

func publicAgentBusy(c *gin.Context, message string) {
	c.Header("Retry-After", "5")
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": message, "code": "INTAKE_BUSY"})
}

func (h *Handlers) publicAgentReceipt(receipt cleanAppWireReceiptResponse) (publicAgentReceiptResponse, error) {
	received, err := time.Parse(time.RFC3339, receipt.ReceivedAt)
	if err != nil {
		return publicAgentReceiptResponse{}, err
	}
	expires := received.Add(publicAgentReceiptTTL)
	if !time.Now().Before(expires) {
		return publicAgentReceiptResponse{}, errPublicReceiptExpired
	}
	token, err := h.discoveryCodec.Seal(publicdiscovery.Payload{Kind: publicdiscovery.KindReceipt, ReceiptID: receipt.ReceiptID, ExpiresAtUnix: expires.Unix()})
	if err != nil {
		return publicAgentReceiptResponse{}, err
	}
	return publicAgentReceiptResponse{ReceiptID: receipt.ReceiptID, Status: "quarantined", PublicationStatus: "private", IdempotencyReplay: receipt.IdempotencyReplay, TrackingToken: token, TrackingExpiresAt: expires.UTC().Format(time.RFC3339), StatusURL: publicAgentPath + "/receipts/" + receipt.ReceiptID, ReceivedAt: receipt.ReceivedAt, ProcessingStatus: "received", Message: "Received privately for review. This receipt does not mean the report is published, routed, or resolved.", Errors: receipt.Errors}, nil
}

func (h *Handlers) GetPublicAgentReceiptV1(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "a valid private receipt tracking token is required"})
		return
	}
	token := strings.TrimPrefix(header, "Bearer ")
	payload, err := h.discoveryCodec.Open(token)
	if err != nil || payload.Kind != publicdiscovery.KindReceipt || payload.ReceiptID == "" || payload.ReceiptID != c.Param("receipt_id") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "a valid private receipt tracking token is required"})
		return
	}
	receipt, err := h.db.GetWireReceipt(c.Request.Context(), publicAgentActor, payload.ReceiptID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "receipt not found"})
			return
		}
		publicAgentBusy(c, "receipt lookup unavailable")
		return
	}
	if !time.Now().Before(receipt.CreatedAt.Add(publicAgentReceiptTTL)) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "private receipt tracking access has expired"})
		return
	}
	resp := publicAgentReceiptResponse{ReceiptID: receipt.ReceiptID, Status: "quarantined", PublicationStatus: "private", IdempotencyReplay: receipt.IdempotencyReplay, StatusURL: publicAgentPath + "/receipts/" + receipt.ReceiptID, ReceivedAt: receipt.CreatedAt.UTC().Format(time.RFC3339), ProcessingStatus: "received", Message: "Received privately for review. This receipt does not mean the report is published, routed, or resolved."}
	if receipt.RejectionCode.Valid && receipt.RejectionCode.String != "" {
		resp.ProcessingStatus = "pending_retry"
		resp.Message = "Report retained privately. Retry the original source_id and payload to resume processing."
		resp.Errors = []cleanAppWireError{{Code: receipt.RejectionCode.String, Message: "processing has not been confirmed"}}
	}
	c.JSON(http.StatusOK, resp)
}
