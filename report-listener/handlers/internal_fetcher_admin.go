package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"report-listener/database"

	"github.com/gin-gonic/gin"
)

type internalListPromotionRequestsResp struct {
	Items []internalPromotionRequestItem `json:"items"`
}

type internalPromotionRequestItem struct {
	ID        int64  `json:"id"`
	FetcherID string `json:"fetcher_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`

	ContactEmail   string `json:"contact_email,omitempty"`
	VerifiedDomain string `json:"verified_domain,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

func (h *Handlers) InternalListPromotionRequests(c *gin.Context) {
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	limit := parseIntParam(c.Query("limit"), 50)

	reqs, err := h.db.ListPromotionRequestsV1(c.Request.Context(), status, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list promotion requests"})
		return
	}

	out := internalListPromotionRequestsResp{Items: make([]internalPromotionRequestItem, 0, len(reqs))}
	for _, r := range reqs {
		item := internalPromotionRequestItem{
			ID:        r.ID,
			FetcherID: r.FetcherID,
			Status:    r.Status,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if r.ContactEmail.Valid {
			item.ContactEmail = r.ContactEmail.String
		}
		if r.VerifiedDomain.Valid {
			item.VerifiedDomain = r.VerifiedDomain.String
		}
		if r.Notes.Valid {
			item.Notes = r.Notes.String
		}
		out.Items = append(out.Items, item)
	}

	c.JSON(http.StatusOK, out)
}

type internalDecidePromotionRequestReq struct {
	Status        string `json:"status"` // approved|denied|needs_info
	DecisionNotes string `json:"decision_notes,omitempty"`
	ReviewedBy    string `json:"reviewed_by,omitempty"` // optional override; otherwise X-Admin-Actor

	Set *internalFetcherGovernanceSet `json:"set,omitempty"`
}

type internalFetcherGovernanceSet struct {
	Tier              *int    `json:"tier,omitempty"`
	ReputationScore   *int    `json:"reputation_score,omitempty"`
	DailyCapItems     *int    `json:"daily_cap_items,omitempty"`
	PerMinuteCapItems *int    `json:"per_minute_cap_items,omitempty"`
	DefaultVisibility *string `json:"default_visibility,omitempty"`
	DefaultTrustLevel *string `json:"default_trust_level,omitempty"`
	RoutingEnabled    *bool   `json:"routing_enabled,omitempty"`
	RewardsEnabled    *bool   `json:"rewards_enabled,omitempty"`
	VerifiedDomain    *string `json:"verified_domain,omitempty"`
	OwnerUserID       *string `json:"owner_user_id,omitempty"`
	Notes             *string `json:"notes,omitempty"`
	FetcherStatus     *string `json:"fetcher_status,omitempty"` // active|limited|suspended|banned
	Active            *bool   `json:"active,omitempty"`
}

type internalDecidePromotionRequestResp struct {
	RequestID int64  `json:"request_id"`
	Status    string `json:"status"`
	FetcherID string `json:"fetcher_id"`
}

func (h *Handlers) InternalDecidePromotionRequest(c *gin.Context) {
	idStr := strings.TrimSpace(c.Param("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req internalDecidePromotionRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	decision := strings.ToLower(strings.TrimSpace(req.Status))
	switch decision {
	case "approved", "denied", "needs_info":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	actor := strings.TrimSpace(req.ReviewedBy)
	if actor == "" {
		actor = strings.TrimSpace(c.GetHeader("X-Admin-Actor"))
	}
	if actor == "" {
		actor = "internal_admin"
	}

	fetcherID, err := h.decidePromotionRequestTx(c.Request.Context(), id, decision, actor, strings.TrimSpace(req.DecisionNotes), req.Set)
	if err != nil {
		if strings.Contains(err.Error(), "not pending") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decide promotion request"})
		return
	}

	_ = h.db.InsertModerationEvent(c.Request.Context(), database.ModerationEvent{
		Actor:      actor,
		ActorIP:    c.ClientIP(),
		Action:     "promotion_request_decide",
		TargetType: "fetcher_promotion_request",
		TargetID:   fmt.Sprintf("%d", id),
		Details: map[string]any{
			"fetcher_id": fetcherID,
			"status":     decision,
		},
		RequestID: c.GetHeader("X-Request-Id"),
	})

	c.JSON(http.StatusOK, internalDecidePromotionRequestResp{RequestID: id, Status: decision, FetcherID: fetcherID})
}

func nullableStrArg(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func (h *Handlers) decidePromotionRequestTx(ctx context.Context, id int64, decision, actor, decisionNotes string, set *internalFetcherGovernanceSet) (string, error) {
	tx, err := h.db.DB().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var fetcherID string
	var curStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT fetcher_id, status
		FROM fetcher_promotion_requests
		WHERE id = ?
		FOR UPDATE
	`, id).Scan(&fetcherID, &curStatus); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("not found")
		}
		return "", err
	}
	if strings.ToLower(strings.TrimSpace(curStatus)) != "pending" {
		return "", fmt.Errorf("not pending (current=%s)", curStatus)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fetcher_promotion_requests
		SET status = ?, decision_notes = ?, reviewed_by = ?, reviewed_at = NOW()
		WHERE id = ?
	`, decision, nullableStrArg(decisionNotes), nullableStrArg(actor), id); err != nil {
		return "", err
	}

	if decision == "approved" {
		if set == nil {
			return "", fmt.Errorf("approved requires set{} fields")
		}
		updates := make([]string, 0, 16)
		args := make([]any, 0, 16)

		if set.Tier != nil {
			updates = append(updates, "tier = ?")
			args = append(args, *set.Tier)
		}
		if set.ReputationScore != nil {
			updates = append(updates, "reputation_score = ?")
			args = append(args, *set.ReputationScore)
		}
		if set.DailyCapItems != nil {
			updates = append(updates, "daily_cap_items = ?")
			args = append(args, *set.DailyCapItems)
		}
		if set.PerMinuteCapItems != nil {
			updates = append(updates, "per_minute_cap_items = ?")
			args = append(args, *set.PerMinuteCapItems)
		}
		if set.DefaultVisibility != nil {
			updates = append(updates, "default_visibility = ?")
			args = append(args, normalizeVisibility(*set.DefaultVisibility, "shadow"))
		}
		if set.DefaultTrustLevel != nil {
			updates = append(updates, "default_trust_level = ?")
			args = append(args, normalizeTrustLevel(*set.DefaultTrustLevel, "unverified"))
		}
		if set.RoutingEnabled != nil {
			updates = append(updates, "routing_enabled = ?")
			args = append(args, *set.RoutingEnabled)
		}
		if set.RewardsEnabled != nil {
			updates = append(updates, "rewards_enabled = ?")
			args = append(args, *set.RewardsEnabled)
		}
		if set.VerifiedDomain != nil {
			updates = append(updates, "verified_domain = ?")
			args = append(args, nullableStrArg(*set.VerifiedDomain))
		}
		if set.OwnerUserID != nil {
			updates = append(updates, "owner_user_id = ?")
			args = append(args, nullableStrArg(*set.OwnerUserID))
		}
		if set.Notes != nil {
			updates = append(updates, "notes = ?")
			args = append(args, nullableStrArg(*set.Notes))
		}
		if set.FetcherStatus != nil {
			updates = append(updates, "status = ?")
			args = append(args, strings.ToLower(strings.TrimSpace(*set.FetcherStatus)))
		}
		if set.Active != nil {
			updates = append(updates, "active = ?")
			args = append(args, *set.Active)
		}

		if len(updates) == 0 {
			return "", fmt.Errorf("approved requires at least one set{} field")
		}

		args = append(args, fetcherID)
		q := fmt.Sprintf("UPDATE fetchers SET %s WHERE fetcher_id = ?", strings.Join(updates, ", "))
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fetcherID, nil
}
