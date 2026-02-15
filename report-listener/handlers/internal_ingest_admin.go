package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"report-listener/database"

	"github.com/gin-gonic/gin"
)

type promoteReportReq struct {
	Visibility string `json:"visibility"`
	TrustLevel string `json:"trust_level"`
}

type promoteReportResp struct {
	Seq        int    `json:"seq"`
	Visibility string `json:"visibility"`
	TrustLevel string `json:"trust_level"`

	PublishedAnalysed bool   `json:"published_analysed"`
	PublishError      string `json:"publish_error,omitempty"`
}

func (h *Handlers) InternalPromoteReport(c *gin.Context) {
	seqStr := c.Param("seq")
	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid seq"})
		return
	}

	var req promoteReportReq
	_ = c.ShouldBindJSON(&req)

	vis := normalizeVisibility(req.Visibility, "public")
	trust := normalizeTrustLevel(req.TrustLevel, "verified")

	// Read previous visibility (to decide whether this is a genuine promotion).
	prevVis := "public"
	{
		var v sql.NullString
		err := h.db.DB().QueryRowContext(c.Request.Context(), `
			SELECT visibility FROM report_raw WHERE report_seq = ?
		`, seq).Scan(&v)
		if err == nil && v.Valid && strings.TrimSpace(v.String) != "" {
			prevVis = normalizeVisibility(v.String, "public")
		}
	}

	// Upsert visibility/trust.
	_, err = h.db.DB().ExecContext(c.Request.Context(), `
		INSERT INTO report_raw (report_seq, visibility, trust_level)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE visibility=VALUES(visibility), trust_level=VALUES(trust_level), updated_at=NOW()
	`, seq, vis, trust)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}

	// Mark promotion moment (used to enable retrying publish if it fails).
	if vis == "public" && prevVis != "public" {
		_, _ = h.db.DB().ExecContext(c.Request.Context(), `
			UPDATE report_raw
			SET promoted_to_public_at = IFNULL(promoted_to_public_at, NOW())
			WHERE report_seq = ?
		`, seq)
	}

	resp := promoteReportResp{Seq: seq, Visibility: vis, TrustLevel: trust}

	// If this report was promoted from quarantine and we haven't published report.analysed yet,
	// publish it now so downstream caches (e.g., fast renderer) update without re-running LLM.
	if vis == "public" {
		var promotedAt sql.NullTime
		var analysedAt sql.NullTime
		_ = h.db.DB().QueryRowContext(c.Request.Context(), `
			SELECT promoted_to_public_at, analysed_published_at
			FROM report_raw
			WHERE report_seq = ?
		`, seq).Scan(&promotedAt, &analysedAt)

		if promotedAt.Valid && !analysedAt.Valid {
			if err := h.publishAnalysedFromDB(c.Request.Context(), seq); err != nil {
				resp.PublishedAnalysed = false
				resp.PublishError = err.Error()
			} else {
				resp.PublishedAnalysed = true
				_, _ = h.db.DB().ExecContext(c.Request.Context(), `
					UPDATE report_raw SET analysed_published_at = NOW() WHERE report_seq = ?
				`, seq)
			}
		}
	}

	_ = h.db.InsertModerationEvent(c.Request.Context(), database.ModerationEvent{
		Actor:      strings.TrimSpace(c.GetHeader("X-Admin-Actor")),
		ActorIP:    c.ClientIP(),
		Action:     "report_promote",
		TargetType: "report",
		TargetID:   seqStr,
		Details: map[string]any{
			"visibility":         vis,
			"trust_level":        trust,
			"published_analysed": resp.PublishedAnalysed,
		},
		RequestID: c.GetHeader("X-Request-Id"),
	})

	c.JSON(http.StatusOK, resp)
}

type analysedPublishReport struct {
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	ID          string    `json:"id"`
	Team        int       `json:"team"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	ActionID    string    `json:"action_id"`
	Description string    `json:"description"`
}

type analysedPublishAnalysis struct {
	Seq                   int       `json:"seq"`
	Source                string    `json:"source"`
	AnalysisText          string    `json:"analysis_text"`
	Title                 string    `json:"title"`
	Description           string    `json:"description"`
	BrandName             string    `json:"brand_name"`
	BrandDisplayName      string    `json:"brand_display_name"`
	LitterProbability     float64   `json:"litter_probability"`
	HazardProbability     float64   `json:"hazard_probability"`
	DigitalBugProbability float64   `json:"digital_bug_probability"`
	SeverityLevel         float64   `json:"severity_level"`
	Summary               string    `json:"summary"`
	Language              string    `json:"language"`
	Classification        string    `json:"classification"`
	IsValid               bool      `json:"is_valid"`
	InferredContactEmails string    `json:"inferred_contact_emails"`
	LegalRiskEstimate     string    `json:"legal_risk_estimate,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type analysedPublishReportWithAnalysis struct {
	Report   analysedPublishReport     `json:"report"`
	Analysis []analysedPublishAnalysis `json:"analysis"`
}

func (h *Handlers) publishAnalysedFromDB(ctx context.Context, seq int) error {
	if h.rabbitmqPublisher == nil || !h.rabbitmqPublisher.IsConnected() {
		return fmt.Errorf("rabbitmq publisher not connected")
	}

	// Load report.
	var (
		ts       time.Time
		id       string
		team     int
		lat      float64
		lng      float64
		x        float64
		y        float64
		actionID sql.NullString
		desc     sql.NullString
	)
	if err := h.db.DB().QueryRowContext(ctx, `
		SELECT seq, ts, id, team, latitude, longitude, x, y, action_id, description
		FROM reports
		WHERE seq = ?
	`, seq).Scan(&seq, &ts, &id, &team, &lat, &lng, &x, &y, &actionID, &desc); err != nil {
		return fmt.Errorf("load report: %w", err)
	}

	r := analysedPublishReport{
		Seq:         seq,
		Timestamp:   ts,
		ID:          id,
		Team:        team,
		Latitude:    lat,
		Longitude:   lng,
		X:           x,
		Y:           y,
		ActionID:    strings.TrimSpace(actionID.String),
		Description: strings.TrimSpace(desc.String),
	}

	// Load analyses.
	rows, err := h.db.DB().QueryContext(ctx, `
		SELECT
			seq, source, analysis_text, title, description,
			brand_name, brand_display_name,
			COALESCE(litter_probability, 0.0) AS litter_probability,
			COALESCE(hazard_probability, 0.0) AS hazard_probability,
			COALESCE(digital_bug_probability, 0.0) AS digital_bug_probability,
			COALESCE(severity_level, 0.0) AS severity_level,
			COALESCE(summary, '') AS summary,
			language, classification, is_valid,
			COALESCE(inferred_contact_emails, '') AS inferred_contact_emails,
			COALESCE(legal_risk_estimate, '') AS legal_risk_estimate,
			created_at, updated_at
		FROM report_analysis
		WHERE seq = ?
	`, seq)
	if err != nil {
		return fmt.Errorf("load analysis: %w", err)
	}
	defer rows.Close()

	var analyses []analysedPublishAnalysis
	for rows.Next() {
		var a analysedPublishAnalysis
		if err := rows.Scan(
			&a.Seq,
			&a.Source,
			&a.AnalysisText,
			&a.Title,
			&a.Description,
			&a.BrandName,
			&a.BrandDisplayName,
			&a.LitterProbability,
			&a.HazardProbability,
			&a.DigitalBugProbability,
			&a.SeverityLevel,
			&a.Summary,
			&a.Language,
			&a.Classification,
			&a.IsValid,
			&a.InferredContactEmails,
			&a.LegalRiskEstimate,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return fmt.Errorf("scan analysis: %w", err)
		}
		analyses = append(analyses, a)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate analysis: %w", err)
	}
	if len(analyses) == 0 {
		return fmt.Errorf("no analysis rows for seq=%d", seq)
	}

	msg := analysedPublishReportWithAnalysis{
		Report:   r,
		Analysis: analyses,
	}

	if err := h.rabbitmqPublisher.PublishWithRoutingKey(h.cfg.RabbitAnalysedReportRoutingKey, msg); err != nil {
		return fmt.Errorf("publish report.analysed: %w", err)
	}

	log.Printf("internal promote: published report.analysed seq=%d analyses=%d", seq, len(analyses))
	return nil
}

type suspendFetcherReq struct {
	Status string `json:"status"`
}

func (h *Handlers) InternalSuspendFetcher(c *gin.Context) {
	fetcherID := strings.TrimSpace(c.Param("fetcher_id"))
	if fetcherID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fetcher_id"})
		return
	}
	var req suspendFetcherReq
	_ = c.ShouldBindJSON(&req)

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = "suspended"
	}
	switch status {
	case "limited", "suspended", "banned":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	_, err := h.db.DB().ExecContext(c.Request.Context(), `
		UPDATE fetchers SET status=?, active=FALSE WHERE fetcher_id=?
	`, status, fetcherID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}

	_ = h.db.InsertModerationEvent(c.Request.Context(), database.ModerationEvent{
		Actor:      strings.TrimSpace(c.GetHeader("X-Admin-Actor")),
		ActorIP:    c.ClientIP(),
		Action:     "fetcher_suspend",
		TargetType: "fetcher",
		TargetID:   fetcherID,
		Details: map[string]any{
			"status": status,
		},
		RequestID: c.GetHeader("X-Request-Id"),
	})

	c.JSON(http.StatusOK, gin.H{"fetcher_id": fetcherID, "status": status})
}

func (h *Handlers) InternalRevokeFetcherKey(c *gin.Context) {
	keyID := strings.TrimSpace(c.Param("key_id"))
	if keyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key_id"})
		return
	}
	_, err := h.db.DB().ExecContext(c.Request.Context(), `
		UPDATE fetcher_keys SET status='revoked' WHERE key_id=?
	`, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
		return
	}

	_ = h.db.InsertModerationEvent(c.Request.Context(), database.ModerationEvent{
		Actor:      strings.TrimSpace(c.GetHeader("X-Admin-Actor")),
		ActorIP:    c.ClientIP(),
		Action:     "fetcher_key_revoke",
		TargetType: "fetcher_key",
		TargetID:   keyID,
		RequestID:  c.GetHeader("X-Request-Id"),
	})

	c.JSON(http.StatusOK, gin.H{"key_id": keyID, "status": "revoked"})
}
