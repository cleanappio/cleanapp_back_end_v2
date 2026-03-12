package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"report-listener/models"
	"report-listener/publicid"
)

const reportContactRefreshTTL = 12 * time.Hour

func (h *Handlers) GetReportContactStrategyBySeq(c *gin.Context) {
	seqStr := strings.TrimSpace(c.Query("seq"))
	if seqStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'seq' parameter"})
		return
	}
	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'seq' parameter. Must be a positive integer."})
		return
	}
	reportWithAnalysis, err := h.db.GetReportBySeq(c.Request.Context(), seq)
	if err != nil {
		h.respondReportContactStrategyError(c, err)
		return
	}
	h.respondReportContactStrategy(c, reportWithAnalysis)
}

func (h *Handlers) GetReportContactStrategyByPublicID(c *gin.Context) {
	publicID := strings.TrimSpace(c.Query("public_id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'public_id' parameter"})
		return
	}
	if !publicid.IsReportID(publicID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'public_id' parameter"})
		return
	}
	reportWithAnalysis, err := h.db.GetReportByPublicID(c.Request.Context(), publicID)
	if err != nil {
		h.respondReportContactStrategyError(c, err)
		return
	}
	h.respondReportContactStrategy(c, reportWithAnalysis)
}

func (h *Handlers) respondReportContactStrategy(c *gin.Context, reportWithAnalysis *models.ReportWithAnalysis) {
	refresh := strings.EqualFold(strings.TrimSpace(c.Query("refresh_targets")), "1")
	strategy, err := h.buildReportContactStrategy(c.Request.Context(), reportWithAnalysis, refresh)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build report contact strategy"})
		return
	}
	c.JSON(http.StatusOK, strategy)
}

func (h *Handlers) respondReportContactStrategyError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve report"})
	}
}

func (h *Handlers) buildReportContactStrategy(ctx context.Context, reportWithAnalysis *models.ReportWithAnalysis, forceRefresh bool) (*models.ReportContactStrategyResponse, error) {
	if reportWithAnalysis == nil {
		return nil, fmt.Errorf("report is required")
	}

	storedTargets, err := h.db.ListReportEscalationTargets(ctx, reportWithAnalysis.Report.Seq)
	if err != nil {
		return nil, err
	}

	refreshed := false
	stale := false
	needsRefresh := forceRefresh || len(storedTargets) == 0
	if !needsRefresh {
		fresh, freshnessErr := h.db.ReportEscalationTargetsFresh(ctx, reportWithAnalysis.Report.Seq, int(reportContactRefreshTTL.Seconds()))
		if freshnessErr == nil && !fresh {
			needsRefresh = true
		}
	}

	if needsRefresh {
		refreshedTargets, refreshErr := h.refreshReportEscalationTargets(ctx, reportWithAnalysis, storedTargets)
		if refreshErr == nil {
			storedTargets = refreshedTargets
			refreshed = true
		} else {
			stale = true
			if len(storedTargets) == 0 {
				storedTargets, _ = h.seedReportEscalationTargets(ctx, reportWithAnalysis, 12)
				storedTargets = routeReportEscalationTargets(reportWithAnalysis, storedTargets)
			}
		}
	}

	visibleTargets := filterVisibleCaseTargets(routeReportEscalationTargets(reportWithAnalysis, storedTargets))
	observations := buildReportContactObservations(reportWithAnalysis.Report.Seq, visibleTargets)
	notifyPlan := buildReportNotifyPlan(reportWithAnalysis, visibleTargets, observations)

	reportWithAnalysis.EscalationTargets = visibleTargets
	reportWithAnalysis.ContactObservations = observations
	reportWithAnalysis.NotifyPlan = notifyPlan
	reportWithAnalysis.ContactStrategyStale = stale

	return &models.ReportContactStrategyResponse{
		ReportSeq:            reportWithAnalysis.Report.Seq,
		PublicID:             reportWithAnalysis.Report.PublicID,
		EscalationTargets:    visibleTargets,
		ContactObservations:  observations,
		NotifyPlan:           notifyPlan,
		Refreshed:            refreshed,
		ContactStrategyStale: stale,
	}, nil
}

func (h *Handlers) refreshReportEscalationTargets(ctx context.Context, reportWithAnalysis *models.ReportWithAnalysis, existing []models.CaseEscalationTarget) ([]models.CaseEscalationTarget, error) {
	if reportWithAnalysis == nil {
		return nil, fmt.Errorf("report is required")
	}

	targets := existing
	if len(targets) == 0 {
		seededTargets, err := h.seedReportEscalationTargets(ctx, reportWithAnalysis, 12)
		if err == nil {
			targets = seededTargets
		}
	}

	if h.contactDiscoverer != nil {
		enrichCtx, cancel := context.WithTimeout(ctx, caseTargetRefreshTimeout)
		defer cancel()
		targets = h.contactDiscoverer.EnrichTargets(enrichCtx, []models.ReportWithAnalysis{*reportWithAnalysis}, targets, 12)
	}
	targets = routeReportEscalationTargets(reportWithAnalysis, targets)

	storedTargets, err := h.db.ReplaceReportEscalationTargets(ctx, reportWithAnalysis.Report.Seq, targets)
	if err != nil {
		return filterVisibleCaseTargets(targets), err
	}
	return storedTargets, nil
}

func (h *Handlers) seedReportEscalationTargets(ctx context.Context, reportWithAnalysis *models.ReportWithAnalysis, limit int) ([]models.CaseEscalationTarget, error) {
	if reportWithAnalysis == nil {
		return nil, fmt.Errorf("report is required")
	}
	geometryJSON := ""
	if reportWithAnalysis.Report.Latitude != 0 || reportWithAnalysis.Report.Longitude != 0 {
		point := map[string]any{
			"type":        "Point",
			"coordinates": []float64{reportWithAnalysis.Report.Longitude, reportWithAnalysis.Report.Latitude},
		}
		payload, err := json.Marshal(point)
		if err == nil {
			geometryJSON = string(payload)
		}
	}
	return h.db.SuggestEscalationTargetsByGeometry(ctx, geometryJSON, []int{reportWithAnalysis.Report.Seq}, limit)
}
