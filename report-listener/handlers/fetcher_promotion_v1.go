package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"report-listener/database"

	"github.com/gin-gonic/gin"
)

type v1CreatePromotionRequestReq struct {
	ContactEmail   string `json:"contact_email"`
	VerifiedDomain string `json:"verified_domain"`

	RequestedTier              *int   `json:"requested_tier,omitempty"`
	RequestedDailyCapItems     *int   `json:"requested_daily_cap_items,omitempty"`
	RequestedPerMinuteCapItems *int   `json:"requested_per_minute_cap_items,omitempty"`
	RequestedDefaultVisibility string `json:"requested_default_visibility,omitempty"`
	RequestedDefaultTrustLevel string `json:"requested_default_trust_level,omitempty"`
	RequestedRoutingEnabled    *bool  `json:"requested_routing_enabled,omitempty"`
	RequestedRewardsEnabled    *bool  `json:"requested_rewards_enabled,omitempty"`

	Notes string `json:"notes"`
}

type v1CreatePromotionRequestResp struct {
	RequestID int64  `json:"request_id"`
	Status    string `json:"status"`
}

func (h *Handlers) CreateFetcherPromotionRequestV1(c *gin.Context) {
	fetcherIDAny, _ := c.Get(middlewareCtxKeyFetcherID())
	keyIDAny, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	fetcherID, _ := fetcherIDAny.(string)
	keyID, _ := keyIDAny.(string)
	if fetcherID == "" || keyID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req v1CreatePromotionRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	notes := strings.TrimSpace(req.Notes)
	if len(notes) < 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notes is required (min 10 chars)"})
		return
	}

	r := &database.FetcherPromotionRequestV1{
		FetcherID: fetcherID,
	}
	if v := strings.TrimSpace(req.ContactEmail); v != "" {
		r.ContactEmail = sql.NullString{String: v, Valid: true}
	}
	if v := strings.TrimSpace(req.VerifiedDomain); v != "" {
		r.VerifiedDomain = sql.NullString{String: v, Valid: true}
	}
	if req.RequestedTier != nil {
		r.RequestedTier = sql.NullInt64{Int64: int64(*req.RequestedTier), Valid: true}
	}
	if req.RequestedDailyCapItems != nil {
		r.RequestedDailyCapItems = sql.NullInt64{Int64: int64(*req.RequestedDailyCapItems), Valid: true}
	}
	if req.RequestedPerMinuteCapItems != nil {
		r.RequestedPerMinuteCapItems = sql.NullInt64{Int64: int64(*req.RequestedPerMinuteCapItems), Valid: true}
	}
	if v := strings.TrimSpace(req.RequestedDefaultVisibility); v != "" {
		r.RequestedDefaultVisibility = sql.NullString{String: v, Valid: true}
	}
	if v := strings.TrimSpace(req.RequestedDefaultTrustLevel); v != "" {
		r.RequestedDefaultTrustLevel = sql.NullString{String: v, Valid: true}
	}
	if req.RequestedRoutingEnabled != nil {
		r.RequestedRoutingEnabled = sql.NullBool{Bool: *req.RequestedRoutingEnabled, Valid: true}
	}
	if req.RequestedRewardsEnabled != nil {
		r.RequestedRewardsEnabled = sql.NullBool{Bool: *req.RequestedRewardsEnabled, Valid: true}
	}
	r.Notes = sql.NullString{String: notes, Valid: true}

	id, err := h.db.CreatePromotionRequestV1(c.Request.Context(), r)
	if err != nil {
		// MVP: detect single-pending limitation.
		if strings.Contains(strings.ToLower(err.Error()), "pending request") {
			c.JSON(http.StatusConflict, gin.H{"error": "pending promotion request already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create promotion request"})
		return
	}

	// best-effort audit
	_ = h.db.InsertModerationEvent(c.Request.Context(), database.ModerationEvent{
		Actor:      "fetcher:" + fetcherID,
		ActorIP:    c.ClientIP(),
		Action:     "promotion_request_create",
		TargetType: "fetcher",
		TargetID:   fetcherID,
		Details: map[string]any{
			"request_id": id,
		},
		RequestID: c.GetHeader("X-Request-Id"),
	})

	c.JSON(http.StatusOK, v1CreatePromotionRequestResp{RequestID: id, Status: "pending"})
}

type v1PromotionStatusResp struct {
	Status  string                        `json:"status"`
	Request *v1PromotionStatusRequestInfo `json:"request,omitempty"`
}

type v1PromotionStatusRequestInfo struct {
	RequestID int64  `json:"request_id"`
	Status    string `json:"status"`

	ContactEmail   string `json:"contact_email,omitempty"`
	VerifiedDomain string `json:"verified_domain,omitempty"`
	Notes          string `json:"notes,omitempty"`
	DecisionNotes  string `json:"decision_notes,omitempty"`
	ReviewedBy     string `json:"reviewed_by,omitempty"`
	ReviewedAt     string `json:"reviewed_at,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func (h *Handlers) GetFetcherPromotionStatusV1(c *gin.Context) {
	fetcherIDAny, _ := c.Get(middlewareCtxKeyFetcherID())
	keyIDAny, _ := c.Get(middlewareCtxKeyFetcherKeyID())
	fetcherID, _ := fetcherIDAny.(string)
	keyID, _ := keyIDAny.(string)
	if fetcherID == "" || keyID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	r, err := h.db.GetLatestPromotionRequestV1(c.Request.Context(), fetcherID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusOK, v1PromotionStatusResp{Status: "none"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load promotion status"})
		return
	}

	out := &v1PromotionStatusRequestInfo{
		RequestID: r.ID,
		Status:    r.Status,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.ContactEmail.Valid {
		out.ContactEmail = r.ContactEmail.String
	}
	if r.VerifiedDomain.Valid {
		out.VerifiedDomain = r.VerifiedDomain.String
	}
	if r.Notes.Valid {
		out.Notes = r.Notes.String
	}
	if r.DecisionNotes.Valid {
		out.DecisionNotes = r.DecisionNotes.String
	}
	if r.ReviewedBy.Valid {
		out.ReviewedBy = r.ReviewedBy.String
	}
	if r.ReviewedAt.Valid {
		out.ReviewedAt = r.ReviewedAt.Time.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, v1PromotionStatusResp{
		Status:  "ok",
		Request: out,
	})
}

// helper for parsing optional ints (used in internal admin endpoints too)
func parseIntParam(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
