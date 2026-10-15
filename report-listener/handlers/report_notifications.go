package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"cleanapp-common/authx"
	"github.com/gin-gonic/gin"
	"report-listener/database"
	"report-listener/models"
	"report-listener/publicid"
)

// Authorization is derived from the verified token subject and shared billing
// records, never a client-supplied user ID, tier, cookie flag, or request header.
func authorizeNotificationSubscriber(c *gin.Context, verify func(context.Context, string) (string, error), subscribed func(context.Context, string) (bool, error)) bool {
	c.Header("Cache-Control", "private, no-store")
	c.Writer.Header().Add("Vary", "Authorization")
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "sign_in_required"})
		return false
	}
	userID, err := verify(c.Request.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_or_expired_token"})
		return false
	}
	active, err := subscribed(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "subscription_check_unavailable"})
		return false
	}
	if !active {
		c.JSON(http.StatusForbidden, gin.H{"error": "subscription_required"})
		return false
	}
	return true
}

func (h *Handlers) requireNotificationSubscriber(c *gin.Context) bool {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	return authorizeNotificationSubscriber(c, func(_ context.Context, token string) (string, error) {
		result, err := authx.VerifyAccessToken(ctx, h.db.DB(), token, h.cfg.JWTSecret)
		if err != nil {
			return "", err
		}
		return result.UserID, nil
	}, func(_ context.Context, userID string) (bool, error) {
		return h.db.HasActiveNotificationSubscription(ctx, userID)
	})
}

func (h *Handlers) GetReportNotificationSummary(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	h.respondReportNotifications(c, false)
}

func (h *Handlers) GetReportNotificationHistory(c *gin.Context) {
	if !h.requireNotificationSubscriber(c) {
		return
	}
	h.respondReportNotifications(c, true)
}

func (h *Handlers) respondReportNotifications(c *gin.Context, full bool) {
	publicID := strings.TrimSpace(c.Query("public_id"))
	if !publicid.IsReportID(publicID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_public_id"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	seq, err := h.db.PublicReportSequence(ctx, publicID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "report_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifications_unavailable"})
		return
	}
	history, err := h.db.ReportNotificationHistory(ctx, seq)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notifications_unavailable"})
		return
	}
	if full {
		c.JSON(http.StatusOK, history)
	} else {
		c.JSON(http.StatusOK, history.ReportNotificationSummary)
	}
}

func (h *Handlers) attachPublicNotificationSummary(ctx context.Context, report *models.ReportWithAnalysis) {
	redactPublicReportContacts(report)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	history, err := h.db.ReportNotificationHistory(ctx, report.Report.Seq)
	if err != nil {
		report.NotificationSummary = database.UnavailableNotificationSummary()
	} else {
		report.NotificationSummary = &history.ReportNotificationSummary
	}
}

func redactPublicReportContacts(report *models.ReportWithAnalysis) {
	// Generated contact suggestions are not a public delivery log. Publishing
	// them here would bypass the subscriber-only recipient history endpoint.
	for index := range report.Analysis {
		report.Analysis[index].InferredContactEmails = ""
		// The legacy raw JSON also contains inferred recipients and internal
		// routing metadata. Public clients have the structured content fields.
		report.Analysis[index].AnalysisText = ""
	}
	report.EscalationTargets = nil
	report.ContactObservations = nil
	report.NotifyPlan = nil
	report.RoutingProfile = nil
	report.ExecutionTasks = nil
	report.NotifyOutcomes = nil
}
