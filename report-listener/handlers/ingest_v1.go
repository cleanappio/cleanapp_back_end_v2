package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"report-listener/database"
	"report-listener/middleware"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const (
	fetcherScopeReportSubmit = "report:submit"
	fetcherScopeFetcherRead  = "fetcher:read"

	defaultBcryptCost = 10
)

type v1RegisterFetcherRequest struct {
	Name      string `json:"name"`
	OwnerType string `json:"owner_type"`
}

type v1RegisterFetcherResponse struct {
	FetcherID string `json:"fetcher_id"`
	APIKey    string `json:"api_key"`
	Status    string `json:"status"`
	Tier      int    `json:"tier"`
	Caps      struct {
		PerMinute int `json:"per_minute_cap_items"`
		Daily     int `json:"daily_cap_items"`
	} `json:"caps"`
	Scopes []string `json:"scopes"`
}

type v1FetcherMeResponse struct {
	FetcherID       string `json:"fetcher_id"`
	Name            string `json:"name"`
	OwnerType       string `json:"owner_type"`
	Status          string `json:"status"`
	Tier            int    `json:"tier"`
	ReputationScore int    `json:"reputation_score"`
	Defaults        struct {
		Visibility     string `json:"default_visibility"`
		TrustLevel     string `json:"default_trust_level"`
		RoutingEnabled bool   `json:"routing_enabled"`
		RewardsEnabled bool   `json:"rewards_enabled"`
	} `json:"defaults"`
	VerifiedDomain string `json:"verified_domain,omitempty"`
	Caps           struct {
		PerMinute int `json:"per_minute_cap_items"`
		Daily     int `json:"daily_cap_items"`
	} `json:"caps"`
	Usage struct {
		MinuteUsed     int `json:"minute_used"`
		DailyUsed      int `json:"daily_used"`
		DailyRemaining int `json:"daily_remaining"`
	} `json:"usage"`
	Scopes     []string `json:"scopes"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`
}

type v1BulkIngestRequest struct {
	Items []v1BulkIngestItem `json:"items"`
}

type v1BulkIngestItem struct {
	SourceID     string              `json:"source_id"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Lat          *float64            `json:"lat,omitempty"`
	Lng          *float64            `json:"lng,omitempty"`
	CollectedAt  string              `json:"collected_at,omitempty"`
	AgentID      string              `json:"agent_id,omitempty"`
	AgentVersion string              `json:"agent_version,omitempty"`
	SourceType   string              `json:"source_type,omitempty"`
	ImageBase64  string              `json:"image_base64,omitempty"`
	Tags         []string            `json:"tags,omitempty"`
	Media        []v1BulkIngestMedia `json:"media,omitempty"`
}

type v1BulkIngestMedia struct {
	URL         string `json:"url,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type v1BulkIngestItemResult struct {
	SourceID   string                     `json:"source_id"`
	Status     string                     `json:"status"` // accepted|duplicate|rejected
	ReportSeq  int                        `json:"report_seq,omitempty"`
	Reason     string                     `json:"reason,omitempty"`
	Queued     bool                       `json:"queued"`
	Visibility string                     `json:"visibility,omitempty"`
	TrustLevel string                     `json:"trust_level,omitempty"`
	Wire       *legacyWireReceiptMetadata `json:"wire,omitempty"`
}

type v1BulkIngestResponse struct {
	Items      []v1BulkIngestItemResult `json:"items"`
	Submitted  int                      `json:"submitted"`
	Accepted   int                      `json:"accepted"`
	Duplicates int                      `json:"duplicates"`
	Rejected   int                      `json:"rejected"`
}

// simple per-IP registration limiter (MVP abuse control)
type regWindow struct {
	WindowStart time.Time
	Count       int
}

var (
	regMu   sync.Mutex
	regByIP = map[string]regWindow{}
)

func allowOwnerType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "internal", "partner", "openclaw":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "unknown"
	}
}

func keyPrefixForEnv(env string) string {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "live", "prod", "production":
		return middleware.FetcherKeyPrefixLive
	default:
		return middleware.FetcherKeyPrefixTest
	}
}

func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// Set version (4) and variant (RFC4122).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexs := hex.EncodeToString(b[:])
	// 8-4-4-4-12
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexs[0:8], hexs[8:12], hexs[12:16], hexs[16:20], hexs[20:32]), nil
}

func randSecretBase64URL(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func placeholderPNGBytes() []byte {
	// 1x1 PNG (same as CI analyzer golden-path).
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5+1WQAAAAASUVORK5CYII="
	b, _ := base64.StdEncoding.DecodeString(pngB64)
	return b
}

func parseRFC3339(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	tt := t.UTC()
	return &tt
}

func clampStr(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func (h *Handlers) RegisterFetcherV1(c *gin.Context) {
	start := time.Now()

	ip := c.ClientIP()
	regMu.Lock()
	w := regByIP[ip]
	now := time.Now()
	if w.WindowStart.IsZero() || now.Sub(w.WindowStart) > time.Hour {
		w = regWindow{WindowStart: now, Count: 0}
	}
	if w.Count >= h.cfg.FetcherRegisterMaxPerHourPerIP {
		regByIP[ip] = w
		regMu.Unlock()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many registrations from this IP, try later"})
		return
	}
	w.Count++
	regByIP[ip] = w
	regMu.Unlock()

	var req v1RegisterFetcherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	fetcherID, err := uuidV4()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate fetcher id"})
		return
	}
	keyID, err := uuidV4()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate key id"})
		return
	}

	name := req.Name
	if strings.TrimSpace(name) == "" {
		name = "anonymous"
	}
	name = clampStr(name, 255)
	ownerType := allowOwnerType(req.OwnerType)

	// Legacy token_hash retained for compatibility; v1 does not use it.
	tokenSeed := sha256.Sum256([]byte(fetcherID + ":" + keyID))
	tokenHash := tokenSeed[:]

	if err := h.db.InsertFetcherV1(c.Request.Context(), fetcherID, name, ownerType, tokenHash); err != nil {
		log.Printf("v1 register: insert fetcher failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register fetcher"})
		return
	}

	secret, err := randSecretBase64URL(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate api key"})
		return
	}

	keyPrefix := keyPrefixForEnv(h.cfg.FetcherKeyEnv)
	apiKey := keyPrefix + keyID + "_" + secret

	keyHashBytes, err := bcrypt.GenerateFromPassword([]byte(secret), defaultBcryptCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate api key"})
		return
	}

	scopes := []string{fetcherScopeReportSubmit, fetcherScopeFetcherRead}
	if err := h.db.InsertFetcherKeyV1(c.Request.Context(), keyID, fetcherID, keyPrefix, string(keyHashBytes), scopes); err != nil {
		log.Printf("v1 register: insert fetcher key failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue api key"})
		return
	}

	var resp v1RegisterFetcherResponse
	resp.FetcherID = fetcherID
	resp.APIKey = apiKey
	resp.Status = "active"
	resp.Tier = 0
	resp.Caps.PerMinute = 20
	resp.Caps.Daily = 200
	resp.Scopes = scopes

	// best-effort audit
	_ = h.db.InsertIngestionAuditV1(c.Request.Context(), fetcherID, keyID, "/v1/fetchers/register", 0, 0, 0, nil, int(time.Since(start).Milliseconds()), ip, c.GetHeader("User-Agent"), c.GetHeader("X-Request-Id"))

	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) GetFetcherMeV1(c *gin.Context) {
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

	// Effective caps (key overrides win).
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

	var resp v1FetcherMeResponse
	resp.FetcherID = fid
	resp.Name = fetcher.Name
	resp.OwnerType = fetcher.OwnerType
	resp.Status = fetcher.Status
	resp.Tier = fetcher.Tier
	resp.ReputationScore = fetcher.ReputationScore
	resp.Defaults.Visibility = normalizeVisibility(fetcher.DefaultVisibility, "shadow")
	resp.Defaults.TrustLevel = normalizeTrustLevel(fetcher.DefaultTrustLevel, "unverified")
	resp.Defaults.RoutingEnabled = fetcher.RoutingEnabled
	resp.Defaults.RewardsEnabled = fetcher.RewardsEnabled
	if fetcher.VerifiedDomain.Valid {
		resp.VerifiedDomain = fetcher.VerifiedDomain.String
	}
	resp.Caps.PerMinute = perMinute
	resp.Caps.Daily = daily
	resp.Usage.MinuteUsed = minUsed
	resp.Usage.DailyUsed = dayUsed
	resp.Usage.DailyRemaining = remaining
	resp.Scopes = key.Scopes
	if fetcher.LastSeenAt.Valid {
		resp.LastSeenAt = fetcher.LastSeenAt.Time.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) BulkIngestV1(c *gin.Context) {
	start := time.Now()

	// Body size guardrail (prevents accidental multi-MB spam payloads).
	if h.cfg.FetcherIngestMaxBodyBytes > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.FetcherIngestMaxBodyBytes)
	}

	fetcherIDVal, _ := c.Get(middlewareCtxKeyFetcherID())
	keyIDVal, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	fetcherID, _ := fetcherIDVal.(string)
	keyID, _ := keyIDVal.(string)
	if fetcherID == "" || keyID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	perMinCapAny, _ := c.Get(middlewareCtxKeyFetcherMinuteCap())
	dailyCapAny, _ := c.Get(middlewareCtxKeyFetcherDailyCap())
	perMinCap, _ := perMinCapAny.(int)
	dailyCap, _ := dailyCapAny.(int)

	// Fetcher-level defaults (promotion workflow can lift a fetcher out of quarantine).
	defVisAny, _ := c.Get(middlewareCtxKeyFetcherDefaultVisibility())
	defTrustAny, _ := c.Get(middlewareCtxKeyFetcherDefaultTrustLevel())
	defVis, _ := defVisAny.(string)
	defTrust, _ := defTrustAny.(string)
	defVis = normalizeVisibility(defVis, "shadow")
	defTrust = normalizeTrustLevel(defTrust, "unverified")

	var req v1BulkIngestRequest
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

	// Enforce quotas based on submitted item count.
	if err := h.db.ConsumeQuotaV1(c.Request.Context(), fetcherID, keyID, time.Now(), len(req.Items), perMinCap, dailyCap); err != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}

	resp := v1BulkIngestResponse{Submitted: len(req.Items)}
	key, fetcher, err := h.db.GetFetcherKeyAndFetcherV1(c.Request.Context(), keyID)
	if err != nil {
		log.Printf("v1 bulkIngest: failed to load fetcher/key context: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	auth := cleanAppWireAuthContext{
		FetcherID: fetcherID,
		KeyID:     keyID,
		Tier:      fetcher.Tier,
		Status:    fetcher.Status,
		Scopes:    key.Scopes,
	}

	serverFailure := false
	for _, it := range req.Items {
		sourceID := strings.TrimSpace(it.SourceID)
		if sourceID == "" {
			resp.Items = append(resp.Items, v1BulkIngestItemResult{
				SourceID: sourceID,
				Status:   "rejected",
				Reason:   "source_id is required",
				Queued:   false,
			})
			resp.Rejected++
			continue
		}
		if len(sourceID) > 255 {
			resp.Items = append(resp.Items, v1BulkIngestItemResult{
				SourceID: sourceID,
				Status:   "rejected",
				Reason:   "source_id too long",
				Queued:   false,
			})
			resp.Rejected++
			continue
		}

		sub := legacyV1ItemToCleanAppWireSubmission(fetcher, it)
		receipt, code := h.processCleanAppWireSubmissionInternal(
			c.Request.Context(),
			auth,
			sub,
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			c.GetHeader("X-Request-Id"),
			"",
		)
		itemResp := v1BulkIngestItemResultFromWireReceipt(receipt)
		resp.Items = append(resp.Items, itemResp)
		switch itemResp.Status {
		case "duplicate":
			resp.Duplicates++
		case "rejected":
			resp.Rejected++
		default:
			resp.Accepted++
		}
		if code >= 500 {
			serverFailure = true
		}
	}

	// best-effort audit
	rejectReasons := map[string]int{}
	for _, it := range resp.Items {
		if it.Status == "rejected" && it.Reason != "" {
			rejectReasons[it.Reason]++
		}
	}
	rejectJSON, _ := json.Marshal(rejectReasons)
	_ = h.db.InsertIngestionAuditV1(c.Request.Context(), fetcherID, keyID, "/v1/reports:bulkIngest", resp.Submitted, resp.Accepted+resp.Duplicates, resp.Rejected, rejectJSON, int(time.Since(start).Milliseconds()), c.ClientIP(), c.GetHeader("User-Agent"), c.GetHeader("X-Request-Id"))

	// If any item hit an internal failure, return 503 so clients can retry safely.
	if serverFailure {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "wire_ingest_failed",
			"details": "one or more items could not be fully ingested; retry the same request safely",
			"result":  resp,
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func legacyV1ItemToCleanAppWireSubmission(fetcher *database.FetcherV1, it v1BulkIngestItem) cleanAppWireSubmission {
	now := time.Now().UTC().Format(time.RFC3339)
	sub := cleanAppWireSubmission{
		SchemaVersion: cleanAppWireSchemaV1,
		SourceID:      strings.TrimSpace(it.SourceID),
		SubmittedAt:   now,
		ObservedAt:    strings.TrimSpace(it.CollectedAt),
	}

	sub.Agent.AgentID = strings.TrimSpace(it.AgentID)
	if sub.Agent.AgentID == "" {
		sub.Agent.AgentID = "legacy-v1-fetcher-" + fetcher.FetcherID
	}
	sub.Agent.AgentName = strings.TrimSpace(fetcher.Name)
	sub.Agent.AgentType = "fetcher"
	sub.Agent.OperatorType = strings.TrimSpace(fetcher.OwnerType)
	sub.Agent.AuthMethod = "api_key"
	sub.Agent.SoftwareVersion = strings.TrimSpace(it.AgentVersion)

	sub.Provenance.GenerationMethod = "legacy_v1_bulk_ingest"
	sub.Provenance.ChainOfCustody = []string{"legacy_v1_bulk_ingest", "wire_adapter"}
	sub.Provenance.HumanInLoop = false

	sub.Report.Title = clampStr(it.Title, 255)
	sub.Report.Description = clampStr(it.Description, 8192)
	if sub.Report.Title == "" {
		sub.Report.Title = sub.Report.Description
	}
	if sub.Report.Title == "" {
		sub.Report.Title = "Legacy machine report"
	}
	sub.Report.Confidence = 0.95
	sub.Report.Tags = append([]string(nil), it.Tags...)
	sub.Report.TargetEntity.Name = strings.TrimSpace(fetcher.Name)
	sub.Report.TargetEntity.TargetType = "organization"
	sub.Report.Language = "en"

	if it.Lat != nil && it.Lng != nil {
		sub.Report.Domain = "physical"
		sub.Report.Location = &struct {
			Kind            string  `json:"kind,omitempty"`
			Lat             float64 `json:"lat"`
			Lng             float64 `json:"lng"`
			Geohash         string  `json:"geohash,omitempty"`
			AddressText     string  `json:"address_text,omitempty"`
			PlaceConfidence float64 `json:"place_confidence,omitempty"`
		}{
			Kind:            "coordinate",
			Lat:             *it.Lat,
			Lng:             *it.Lng,
			PlaceConfidence: 1,
		}
	} else {
		sub.Report.Domain = "digital"
		sub.Report.DigitalContext = map[string]any{
			"legacy_v1":   true,
			"source_type": strings.TrimSpace(it.SourceType),
		}
	}

	sub.Report.ProblemType = normalizeWireSlug(it.SourceType)
	if sub.Report.ProblemType == "" {
		if sub.Report.Domain == "physical" {
			sub.Report.ProblemType = "legacy_physical_ingest"
		} else {
			sub.Report.ProblemType = "legacy_digital_ingest"
		}
	}

	for _, media := range it.Media {
		sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			Type:       "media_link",
			URI:        strings.TrimSpace(media.URL),
			SHA256:     strings.TrimSpace(media.SHA256),
			MIMEType:   strings.TrimSpace(media.ContentType),
			CapturedAt: strings.TrimSpace(it.CollectedAt),
		})
	}
	if strings.TrimSpace(it.ImageBase64) != "" {
		sub.Report.EvidenceBundle = append(sub.Report.EvidenceBundle, struct {
			EvidenceID string `json:"evidence_id,omitempty"`
			Type       string `json:"type"`
			URI        string `json:"uri,omitempty"`
			SHA256     string `json:"sha256,omitempty"`
			MIMEType   string `json:"mime_type,omitempty"`
			CapturedAt string `json:"captured_at,omitempty"`
		}{
			EvidenceID: "inline-image",
			Type:       "inline_image",
			MIMEType:   "application/octet-stream",
			CapturedAt: strings.TrimSpace(it.CollectedAt),
		})
		if sub.Extensions == nil {
			sub.Extensions = map[string]any{}
		}
		sub.Extensions["image_base64"] = strings.TrimSpace(it.ImageBase64)
	}

	return sub
}

func v1BulkIngestItemResultFromWireReceipt(receipt cleanAppWireReceiptResponse) v1BulkIngestItemResult {
	out := v1BulkIngestItemResult{
		SourceID:   receipt.SourceID,
		ReportSeq:  receipt.ReportID,
		Queued:     len(receipt.Errors) == 0,
		Visibility: normalizeVisibility(laneToVisibility(receipt.Lane), "shadow"),
		TrustLevel: normalizeTrustLevel(laneToTrustLevel(receipt.Lane), "unverified"),
		Wire: &legacyWireReceiptMetadata{
			SourceID:       receipt.SourceID,
			ReceiptID:      receipt.ReceiptID,
			SubmissionID:   receipt.SubmissionID,
			Status:         receipt.Status,
			Lane:           receipt.Lane,
			ReportSeq:      receipt.ReportID,
			NextCheckAfter: receipt.NextCheckAfter,
		},
	}
	switch {
	case receipt.IdempotencyReplay:
		out.Status = "duplicate"
	case len(receipt.Errors) > 0:
		out.Status = "rejected"
		out.Queued = false
		out.Visibility = ""
		out.TrustLevel = ""
		if len(receipt.Errors) > 0 {
			out.Reason = receipt.Errors[0].Message
		}
	default:
		out.Status = "accepted"
	}
	return out
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

func normalizeVisibility(v string, def string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "shadow", "limited", "public":
		return v
	default:
	}
	def = strings.ToLower(strings.TrimSpace(def))
	switch def {
	case "shadow", "limited", "public":
		return def
	default:
		return "shadow"
	}
}

func normalizeTrustLevel(v string, def string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "unverified", "verified", "trusted":
		return v
	default:
	}
	def = strings.ToLower(strings.TrimSpace(def))
	switch def {
	case "unverified", "verified", "trusted":
		return def
	default:
		return "unverified"
	}
}

// Gin context key helpers (kept local to avoid exporting internal constants).
func middlewareCtxKeyFetcherID() string        { return "fetcher_id" }
func middlewareCtxKeyFetcherKeyID() string     { return "fetcher_key_id" }
func middlewareCtxKeyFetcherMinuteCap() string { return "fetcher_per_minute_cap_items" }
func middlewareCtxKeyFetcherDailyCap() string  { return "fetcher_daily_cap_items" }
func middlewareCtxKeyFetcherDefaultVisibility() string {
	return "fetcher_default_visibility"
}
func middlewareCtxKeyFetcherDefaultTrustLevel() string {
	return "fetcher_default_trust_level"
}
