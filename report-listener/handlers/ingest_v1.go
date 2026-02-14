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
	Caps            struct {
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
	Items []struct {
		SourceID     string   `json:"source_id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Lat          *float64 `json:"lat,omitempty"`
		Lng          *float64 `json:"lng,omitempty"`
		CollectedAt  string   `json:"collected_at,omitempty"`
		AgentID      string   `json:"agent_id,omitempty"`
		AgentVersion string   `json:"agent_version,omitempty"`
		SourceType   string   `json:"source_type,omitempty"`
		Media        []struct {
			URL         string `json:"url,omitempty"`
			SHA256      string `json:"sha256,omitempty"`
			ContentType string `json:"content_type,omitempty"`
		} `json:"media,omitempty"`
	} `json:"items"`
}

type v1BulkIngestItemResult struct {
	SourceID   string `json:"source_id"`
	Status     string `json:"status"` // accepted|duplicate|rejected
	ReportSeq  int    `json:"report_seq,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Queued     bool   `json:"queued"`
	Visibility string `json:"visibility,omitempty"`
	TrustLevel string `json:"trust_level,omitempty"`
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

	// Collect source IDs for duplicate detection.
	sourceIDs := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		if strings.TrimSpace(it.SourceID) != "" {
			sourceIDs = append(sourceIDs, it.SourceID)
		}
	}
	existing, err := h.db.GetExistingReportSeqsV1(c.Request.Context(), fetcherID, sourceIDs)
	if err != nil {
		log.Printf("v1 bulkIngest: duplicate check failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "duplicate check failed"})
		return
	}

	type prepared struct {
		idx         int
		sourceID    string
		title       string
		description string
		lat         float64
		lng         float64
		collectedAt *time.Time
		agentID     string
		agentVer    string
		sourceType  string
		seq         int
		duplicate   bool
	}

	preparedItems := make([]prepared, 0, len(req.Items))
	resp := v1BulkIngestResponse{Submitted: len(req.Items)}

	for i, it := range req.Items {
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

		lat := 0.0
		lng := 0.0
		if it.Lat != nil {
			lat = *it.Lat
		}
		if it.Lng != nil {
			lng = *it.Lng
		}

		seq, dup := existing[sourceID]
		p := prepared{
			idx:         i,
			sourceID:    sourceID,
			title:       clampStr(it.Title, 255),
			description: clampStr(it.Description, 8192),
			lat:         lat,
			lng:         lng,
			collectedAt: parseRFC3339(it.CollectedAt),
			agentID:     clampStr(it.AgentID, 255),
			agentVer:    clampStr(it.AgentVersion, 64),
			sourceType:  clampStr(it.SourceType, 32),
			seq:         seq,
			duplicate:   dup,
		}
		if p.description == "" {
			p.description = p.title
		}
		preparedItems = append(preparedItems, p)
	}

	// Insert new reports + report_raw rows in a single transaction.
	// Quarantine lane defaults to visibility=shadow.
	newOnes := make([]prepared, 0, len(preparedItems))
	for _, p := range preparedItems {
		if !p.duplicate && p.seq == 0 && p.sourceID != "" {
			newOnes = append(newOnes, p)
		}
	}

	if len(newOnes) > 0 {
		tx, err := h.db.DB().BeginTx(c.Request.Context(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "begin tx failed"})
			return
		}
		defer tx.Rollback()

		reporterID := "fetcher_v1:" + fetcherID
		img := placeholderPNGBytes()

		// Insert reports in chunks (keeps MySQL packet reasonable).
		const chunkSize = 200
		for i := 0; i < len(newOnes); i += chunkSize {
			end := i + chunkSize
			if end > len(newOnes) {
				end = len(newOnes)
			}
			chunk := newOnes[i:end]

			vals := make([]string, 0, len(chunk))
			args := make([]interface{}, 0, len(chunk)*9)
			for _, it := range chunk {
				vals = append(vals, "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
				args = append(args,
					reporterID,
					0, // team unknown
					"",
					it.lat,
					it.lng,
					0.0,
					0.0,
					img,
					clampStr(it.title, 255),
				)
			}

			res, err := tx.ExecContext(c.Request.Context(),
				fmt.Sprintf("INSERT INTO reports (id, team, action_id, latitude, longitude, x, y, image, description) VALUES %s", strings.Join(vals, ",")),
				args...,
			)
			if err != nil {
				log.Printf("v1 bulkIngest: insert reports failed: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "insert reports failed"})
				return
			}
			firstID, _ := res.LastInsertId()
			rows, _ := res.RowsAffected()
			for j := 0; j < int(rows); j++ {
				newOnes[i+j].seq = int(firstID) + j
			}
		}

		// Insert report_raw metadata.
		rawVals := make([]string, 0, len(newOnes))
		rawArgs := make([]interface{}, 0, len(newOnes)*10)
		for _, it := range newOnes {
			rawVals = append(rawVals, "(?, ?, ?, ?, ?, ?, ?, 'shadow', 'unverified', 0)")
			rawArgs = append(rawArgs,
				it.seq,
				fetcherID,
				it.sourceID,
				nullable(it.agentID),
				nullable(it.agentVer),
				nullableTime(it.collectedAt),
				nullable(it.sourceType),
			)
		}

		if _, err := tx.ExecContext(c.Request.Context(),
			fmt.Sprintf("INSERT INTO report_raw (report_seq, fetcher_id, source_id, agent_id, agent_version, collected_at, source_type, visibility, trust_level, spam_score) VALUES %s", strings.Join(rawVals, ",")),
			rawArgs...,
		); err != nil {
			log.Printf("v1 bulkIngest: insert report_raw failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "insert report_raw failed"})
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("v1 bulkIngest: commit failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
			return
		}

		// Update preparedItems seqs for new inserts.
		seqBySource := make(map[string]int, len(newOnes))
		for _, it := range newOnes {
			seqBySource[it.sourceID] = it.seq
		}
		for i := range preparedItems {
			if preparedItems[i].seq == 0 && !preparedItems[i].duplicate {
				if s, ok := seqBySource[preparedItems[i].sourceID]; ok {
					preparedItems[i].seq = s
				}
			}
		}
	}

	// Publish to RabbitMQ for analysis (includes duplicates; idempotent-ish on the analyzer side).
	queuedFailures := 0
	for _, it := range preparedItems {
		if it.sourceID == "" || it.seq == 0 {
			continue
		}
		if h.rabbitmqPublisher == nil || !h.rabbitmqPublisher.IsConnected() {
			queuedFailures++
			continue
		}
		msg := map[string]interface{}{
			"seq":         it.seq,
			"description": it.description,
			"latitude":    it.lat,
			"longitude":   it.lng,
			"fetcher_id":  fetcherID,
			"source_id":   it.sourceID,
			"visibility":  "shadow",
		}
		if err := h.rabbitmqPublisher.PublishWithRoutingKey(h.cfg.RabbitRawReportRoutingKey, msg); err != nil {
			log.Printf("v1 bulkIngest: publish failed seq=%d: %v", it.seq, err)
			queuedFailures++
		}
	}

	// Build response items.
	for _, it := range preparedItems {
		if it.sourceID == "" {
			continue
		}
		if it.seq == 0 && !it.duplicate {
			// rejected already (e.g. missing source_id)
			continue
		}
		status := "accepted"
		if it.duplicate {
			status = "duplicate"
			resp.Duplicates++
		} else {
			resp.Accepted++
		}
		resp.Items = append(resp.Items, v1BulkIngestItemResult{
			SourceID:   it.sourceID,
			Status:     status,
			ReportSeq:  it.seq,
			Queued:     queuedFailures == 0, // coarse-grained (per-request)
			Visibility: "shadow",
			TrustLevel: "unverified",
		})
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

	// If we couldn't queue for analysis, return 503 so clients can retry (idempotency makes it safe).
	if queuedFailures > 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "queued_failed",
			"details": "accepted into quarantine but could not queue for analysis; retry the same request safely",
			"result":  resp,
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

// Gin context key helpers (kept local to avoid exporting internal constants).
func middlewareCtxKeyFetcherID() string        { return "fetcher_id" }
func middlewareCtxKeyFetcherKeyID() string     { return "fetcher_key_id" }
func middlewareCtxKeyFetcherMinuteCap() string { return "fetcher_per_minute_cap_items" }
func middlewareCtxKeyFetcherDailyCap() string  { return "fetcher_daily_cap_items" }
