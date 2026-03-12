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

	"report-listener/middleware"
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

func (h *Handlers) RecordReportExecutionTaskOutcomeBySeq(c *gin.Context) {
	seqStr := strings.TrimSpace(c.Param("seq"))
	seq, err := strconv.Atoi(seqStr)
	if err != nil || seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report seq"})
		return
	}
	reportWithAnalysis, err := h.db.GetReportBySeq(c.Request.Context(), seq)
	if err != nil {
		h.respondReportContactStrategyError(c, err)
		return
	}
	h.recordReportExecutionTaskOutcome(c, reportWithAnalysis)
}

func (h *Handlers) RecordReportExecutionTaskOutcomeByPublicID(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("public_id"))
	if !publicid.IsReportID(publicID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid report public id"})
		return
	}
	reportWithAnalysis, err := h.db.GetReportByPublicID(c.Request.Context(), publicID)
	if err != nil {
		h.respondReportContactStrategyError(c, err)
		return
	}
	h.recordReportExecutionTaskOutcome(c, reportWithAnalysis)
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

func (h *Handlers) recordReportExecutionTaskOutcome(c *gin.Context, reportWithAnalysis *models.ReportWithAnalysis) {
	if reportWithAnalysis == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	taskID, err := parseTaskIDParam(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}
	var req models.RecordNotifyExecutionTaskOutcomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	outcomeType, err := normalizeNotifyOutcomeType(req.OutcomeType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	subjectRef := reportSubjectRef(reportWithAnalysis)
	task, err := h.db.GetNotifyExecutionTask(c.Request.Context(), "report", subjectRef, taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load execution task"})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution task not found"})
		return
	}
	targets, err := h.db.ListReportEscalationTargets(c.Request.Context(), reportWithAnalysis.Report.Seq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load report targets"})
		return
	}
	target, ok := findCaseTargetByID(targets, task.TargetID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution task target not found"})
		return
	}
	actorUserID := middleware.GetUserIDFromContext(c)
	if strings.TrimSpace(req.ActorUserID) != "" {
		actorUserID = strings.TrimSpace(req.ActorUserID)
	}
	completedAt := time.Now().UTC()
	updatedTask, err := h.db.UpdateNotifyExecutionTask(c.Request.Context(), "report", subjectRef, taskID, taskStatusForOutcome(outcomeType), actorUserID, &completedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update execution task"})
		return
	}
	profile := buildReportRoutingProfile(reportWithAnalysis)
	outcome, memory, err := h.recordNotifyOutcomeAndMemory(
		c.Request.Context(),
		"report",
		subjectRef,
		task.TargetID,
		&target,
		profile.AssetClass,
		outcomeType,
		"execution_task",
		firstNonEmpty(updatedTask.Summary, target.DisplayName, target.Organization),
		req.Note,
		completedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record execution outcome"})
		return
	}
	c.JSON(http.StatusOK, models.RecordNotifyExecutionTaskOutcomeResponse{
		Task:           *updatedTask,
		Outcome:        *outcome,
		EndpointMemory: memory,
	})
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

	profile := buildReportRoutingProfile(reportWithAnalysis)
	visibleTargets := filterVisibleCaseTargets(h.routeTargetsForSubject(ctx, profile, storedTargets))
	observations := buildReportContactObservations(reportWithAnalysis.Report.Seq, visibleTargets)
	notifyPlan := buildReportNotifyPlan(reportWithAnalysis, visibleTargets, observations)
	subjectRef := reportSubjectRef(reportWithAnalysis)

	storedProfile, err := h.db.UpsertSubjectRoutingProfile(ctx, subjectRoutingProfileModel("report", subjectRef, profile))
	if err != nil {
		return nil, err
	}
	executionTasks, err := h.db.ReplaceNotifyExecutionTasks(ctx, "report", subjectRef, buildNotifyExecutionTasks("report", subjectRef, visibleTargets, notifyPlan))
	if err != nil {
		return nil, err
	}
	notifyOutcomes, err := h.db.ListNotifyOutcomes(ctx, "report", subjectRef)
	if err != nil {
		return nil, err
	}

	reportWithAnalysis.EscalationTargets = visibleTargets
	reportWithAnalysis.ContactObservations = observations
	reportWithAnalysis.NotifyPlan = notifyPlan
	reportWithAnalysis.RoutingProfile = storedProfile
	reportWithAnalysis.ExecutionTasks = executionTasks
	reportWithAnalysis.NotifyOutcomes = notifyOutcomes
	reportWithAnalysis.ContactStrategyStale = stale

	return &models.ReportContactStrategyResponse{
		ReportSeq:            reportWithAnalysis.Report.Seq,
		PublicID:             reportWithAnalysis.Report.PublicID,
		EscalationTargets:    visibleTargets,
		ContactObservations:  observations,
		NotifyPlan:           notifyPlan,
		RoutingProfile:       storedProfile,
		ExecutionTasks:       executionTasks,
		NotifyOutcomes:       notifyOutcomes,
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
		targets = h.contactDiscoverer.EnrichTargets(enrichCtx, []models.ReportWithAnalysis{*reportWithAnalysis}, targets, 12, h.loadAuthorityDirectoryRules(enrichCtx, []models.ReportWithAnalysis{*reportWithAnalysis}))
	}
	targets = h.routeTargetsForSubject(ctx, buildReportRoutingProfile(reportWithAnalysis), targets)

	storedTargets, err := h.db.ReplaceReportEscalationTargets(ctx, reportWithAnalysis.Report.Seq, targets)
	if err != nil {
		return filterVisibleCaseTargets(targets), err
	}
	return h.routeTargetsForSubject(ctx, buildReportRoutingProfile(reportWithAnalysis), storedTargets), nil
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
