package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"

	"report-listener/middleware"
	"report-listener/models"
)

type internalCaseEscalationRecipient struct {
	TargetID       *int64 `json:"target_id,omitempty"`
	Email          string `json:"email"`
	DeliverySource string `json:"delivery_source"`
	DisplayName    string `json:"display_name,omitempty"`
	Organization   string `json:"organization,omitempty"`
}

type internalCaseEscalationSendRequest struct {
	CaseID     string                            `json:"case_id"`
	Subject    string                            `json:"subject"`
	Body       string                            `json:"body"`
	HTMLBody   string                            `json:"html_body,omitempty"`
	Recipients []internalCaseEscalationRecipient `json:"recipients"`
}

type internalCaseEscalationSendResult struct {
	TargetID          *int64  `json:"target_id,omitempty"`
	Email             string  `json:"email"`
	Status            string  `json:"status"`
	DeliverySource    string  `json:"delivery_source"`
	Provider          string  `json:"provider"`
	ProviderMessageID string  `json:"provider_message_id,omitempty"`
	SentAt            *string `json:"sent_at,omitempty"`
	Error             string  `json:"error,omitempty"`
}

type internalCaseEscalationSendResponse struct {
	CaseID  string                             `json:"case_id"`
	Results []internalCaseEscalationSendResult `json:"results"`
}

func (h *Handlers) GetCaseEscalations(c *gin.Context) {
	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case escalations"})
		return
	}
	if queryBoolParam(c, "refresh_targets") {
		if enriched, enrichErr := h.enrichCaseEscalationTargets(c.Request.Context(), detail); enrichErr != nil {
			log.Printf("warn: case escalation target refresh failed for %s: %v", c.Param("case_id"), enrichErr)
		} else if len(enriched) > 0 {
			detail.EscalationTargets = enriched
		}
	}
	if err := h.syncCaseContactStrategy(c.Request.Context(), detail); err != nil {
		log.Printf("warn: case contact routing sync failed for %s: %v", c.Param("case_id"), err)
	}
	c.JSON(http.StatusOK, gin.H{
		"case_id":         detail.Case.CaseID,
		"targets":         detail.EscalationTargets,
		"observations":    detail.ContactObservations,
		"notify_plan":     detail.NotifyPlan,
		"routing_profile": detail.RoutingProfile,
		"execution_tasks": detail.ExecutionTasks,
		"notify_outcomes": detail.NotifyOutcomes,
		"actions":         detail.EscalationActions,
		"deliveries":      detail.EmailDeliveries,
		"linked_count":    len(detail.LinkedReports),
	})
}

func (h *Handlers) RecordCaseExecutionTaskOutcome(c *gin.Context) {
	taskID, err := parseTaskIDParam(c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}
	caseID := strings.TrimSpace(c.Param("case_id"))
	if caseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing case id"})
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
	actorUserID := middleware.GetUserIDFromContext(c)
	if strings.TrimSpace(req.ActorUserID) != "" {
		actorUserID = strings.TrimSpace(req.ActorUserID)
	}

	task, err := h.db.GetNotifyExecutionTask(c.Request.Context(), "case", caseID, taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load execution task"})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution task not found"})
		return
	}

	detail, err := h.db.GetCaseDetail(c.Request.Context(), caseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}
	target, ok := findCaseTargetByID(detail.EscalationTargets, task.TargetID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution task target not found"})
		return
	}

	completedAt := time.Now().UTC()
	updatedTask, err := h.db.UpdateNotifyExecutionTask(c.Request.Context(), "case", caseID, taskID, taskStatusForOutcome(outcomeType), actorUserID, &completedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update execution task"})
		return
	}

	preferredAssetClass := buildCaseRoutingProfile(detail).AssetClass
	if detail.RoutingProfile != nil && strings.TrimSpace(detail.RoutingProfile.AssetClass) != "" {
		preferredAssetClass = strings.TrimSpace(detail.RoutingProfile.AssetClass)
	}
	outcome, memory, err := h.recordNotifyOutcomeAndMemory(
		c.Request.Context(),
		"case",
		caseID,
		task.TargetID,
		&target,
		preferredAssetClass,
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

	if err := h.recordCaseExecutionOutcomeSideEffects(c.Request.Context(), detail, updatedTask, target, outcomeType, actorUserID, req.Note); err != nil {
		log.Printf("warn: failed to record case execution side effects for %s task %d: %v", caseID, taskID, err)
	}

	c.JSON(http.StatusOK, models.RecordNotifyExecutionTaskOutcomeResponse{
		Task:           *updatedTask,
		Outcome:        *outcome,
		EndpointMemory: memory,
	})
}

func (h *Handlers) DraftCaseEscalation(c *gin.Context) {
	var req models.DraftCaseEscalationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !isEmptyJSONBodyError(err) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}
	targets := selectCaseTargets(detail.EscalationTargets, req.TargetIDs)
	ccEmails := normalizeManualCCEmails(req.CCEmails, targets)
	subject, body := buildCaseEscalationDraft(detail, targets, req.Subject, req.Body)
	c.JSON(http.StatusOK, models.CaseEscalationDraftResponse{
		CaseID:      detail.Case.CaseID,
		Subject:     subject,
		Body:        body,
		CCEmails:    ccEmails,
		Targets:     targets,
		LinkedCount: len(detail.LinkedReports),
	})
}

func (h *Handlers) SendCaseEscalation(c *gin.Context) {
	var req models.SendCaseEscalationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	detail, err := h.db.GetCaseDetail(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load case"})
		return
	}

	targets := selectCaseTargets(detail.EscalationTargets, req.TargetIDs)
	if len(targets) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no email-capable escalation targets selected"})
		return
	}

	ccRecipients := buildManualCCRecipients(req.CCEmails, targets)
	ccEmails := make([]string, 0, len(ccRecipients))
	for _, recipient := range ccRecipients {
		ccEmails = append(ccEmails, recipient.Email)
	}

	subject, body := buildCaseEscalationDraft(detail, targets, req.Subject, req.Body)
	actorUserID := middleware.GetUserIDFromContext(c)
	if strings.TrimSpace(req.ActorUserID) != "" {
		actorUserID = req.ActorUserID
	}

	selectedTargets, actions, err := h.db.CreateCaseEscalationActions(c.Request.Context(), detail.Case.CaseID, req.TargetIDs, subject, body, actorUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create escalation actions"})
		return
	}

	sendResults, err := h.sendCaseEscalation(c, detail.Case.CaseID, body, subject, selectedTargets, ccRecipients)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to send case escalation emails"})
		return
	}

	deliveries := make([]models.CaseEmailDelivery, 0, len(sendResults))
	for _, result := range sendResults {
		delivery := models.CaseEmailDelivery{
			CaseID:            detail.Case.CaseID,
			TargetID:          result.TargetID,
			RecipientEmail:    result.Email,
			DeliveryStatus:    result.Status,
			DeliverySource:    emptyStringDefault(result.DeliverySource, "case_target"),
			Provider:          emptyStringDefault(result.Provider, "sendgrid"),
			ProviderMessageID: result.ProviderMessageID,
			ErrorMessage:      result.Error,
		}
		if result.TargetID != nil {
			for _, action := range actions {
				if action.TargetID != nil && *action.TargetID == *result.TargetID {
					actionID := action.ID
					delivery.ActionID = &actionID
					break
				}
			}
		}
		if result.SentAt != nil {
			parsed, err := time.Parse(time.RFC3339, *result.SentAt)
			if err == nil {
				ts := parsed.UTC()
				delivery.SentAt = &ts
			}
		}
		deliveries = append(deliveries, delivery)
	}

	if err := h.db.RecordCaseEscalationDeliveries(c.Request.Context(), detail.Case.CaseID, actions, deliveries, actorUserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist escalation deliveries"})
		return
	}
	if err := h.recordCaseDeliveryOutcomes(c.Request.Context(), detail.Case.CaseID, selectedTargets, deliveries); err != nil {
		log.Printf("warn: failed to record notify outcomes for %s: %v", detail.Case.CaseID, err)
	}

	c.JSON(http.StatusOK, models.CaseEscalationSendResponse{
		CaseID:     detail.Case.CaseID,
		Subject:    subject,
		Body:       body,
		CCEmails:   ccEmails,
		Actions:    actions,
		Deliveries: deliveries,
	})
}

func (h *Handlers) recordCaseDeliveryOutcomes(ctx context.Context, caseID string, targets []models.CaseEscalationTarget, deliveries []models.CaseEmailDelivery) error {
	targetsByID := make(map[int64]models.CaseEscalationTarget, len(targets))
	for _, target := range targets {
		targetsByID[target.ID] = target
	}
	for _, delivery := range deliveries {
		var target models.CaseEscalationTarget
		if delivery.TargetID != nil {
			target = targetsByID[*delivery.TargetID]
		}
		endpointKey := endpointKeyForTarget(target)
		outcomeType := "sent"
		switch strings.ToLower(strings.TrimSpace(delivery.DeliveryStatus)) {
		case "sent", "delivered":
			outcomeType = "sent"
		case "failed", "bounced", "invalid_email":
			outcomeType = "bounced"
		default:
			outcomeType = strings.ToLower(strings.TrimSpace(delivery.DeliveryStatus))
		}
		if err := h.db.RecordNotifyOutcome(ctx, models.NotifyOutcome{
			SubjectKind: "case",
			SubjectRef:  caseID,
			TargetID:    delivery.TargetID,
			EndpointKey: endpointKey,
			OutcomeType: outcomeType,
			SourceType:  "case_email_delivery",
			SourceRef:   delivery.RecipientEmail,
			EvidenceJSON: fmt.Sprintf(
				`{"recipient_email":"%s","delivery_status":"%s","provider":"%s"}`,
				escapeJSONString(delivery.RecipientEmail),
				escapeJSONString(delivery.DeliveryStatus),
				escapeJSONString(delivery.Provider),
			),
		}); err != nil {
			return err
		}
		endpointMemoryKey := firstNonEmpty(endpointKey, strings.ToLower(strings.TrimSpace(delivery.RecipientEmail)))
		existingMemory, err := h.db.GetContactEndpointMemory(ctx, endpointMemoryKey)
		if err != nil {
			return err
		}
		memory := models.ContactEndpointMemory{
			EndpointKey:            endpointMemoryKey,
			OrganizationKey:        target.OrganizationKey,
			ChannelType:            "email",
			ChannelValue:           strings.ToLower(strings.TrimSpace(delivery.RecipientEmail)),
			LastResult:             outcomeType,
			PreferredForRoleType:   target.RoleType,
			PreferredForAssetClass: "",
		}
		if existingMemory != nil {
			memory.SuccessCount = existingMemory.SuccessCount
			memory.BounceCount = existingMemory.BounceCount
			memory.AckCount = existingMemory.AckCount
			memory.FixCount = existingMemory.FixCount
			memory.MisrouteCount = existingMemory.MisrouteCount
			memory.NoResponseCount = existingMemory.NoResponseCount
		}
		switch outcomeType {
		case "sent":
			memory.SuccessCount++
		case "bounced":
			memory.BounceCount++
			memory.CooldownUntil = notifyOutcomeCooldown(outcomeType, time.Now().UTC())
		case "misrouted":
			memory.MisrouteCount++
			memory.CooldownUntil = notifyOutcomeCooldown(outcomeType, time.Now().UTC())
		}
		if sentAt := delivery.SentAt; sentAt != nil {
			memory.LastContactedAt = sentAt
		} else {
			now := time.Now().UTC()
			memory.LastContactedAt = &now
		}
		if _, err := h.db.UpsertContactEndpointMemory(ctx, memory); err != nil {
			return err
		}
	}
	return nil
}

func normalizeNotifyOutcomeType(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ack", "acknowledged":
		return "acknowledged", nil
	case "fixed", "resolved":
		return "fixed", nil
	case "misrouted", "wrong_recipient":
		return "misrouted", nil
	case "no_response", "no-response", "noresponse":
		return "no_response", nil
	case "bounced", "bounce":
		return "bounced", nil
	case "sent", "delivered":
		return "sent", nil
	default:
		return "", fmt.Errorf("unsupported outcome type")
	}
}

func taskStatusForOutcome(outcomeType string) string {
	switch outcomeType {
	case "no_response":
		return "completed"
	case "misrouted":
		return "completed"
	case "acknowledged", "fixed", "bounced", "sent":
		return "completed"
	default:
		return "completed"
	}
}

func notifyOutcomeCooldown(outcomeType string, now time.Time) *time.Time {
	var duration time.Duration
	switch outcomeType {
	case "bounced":
		duration = 14 * 24 * time.Hour
	case "misrouted":
		duration = 7 * 24 * time.Hour
	case "acknowledged":
		duration = 72 * time.Hour
	case "fixed":
		duration = 30 * 24 * time.Hour
	default:
		return nil
	}
	ts := now.Add(duration)
	return &ts
}

func endpointChannelValue(target models.CaseEscalationTarget) string {
	switch caseTargetChannel(target) {
	case "email":
		return strings.ToLower(strings.TrimSpace(target.Email))
	case "phone":
		return strings.TrimSpace(target.Phone)
	case "website":
		return firstNonEmpty(strings.TrimSpace(target.ContactURL), strings.TrimSpace(target.Website))
	case "social":
		return firstNonEmpty(strings.TrimSpace(target.ContactURL), strings.TrimSpace(target.SocialHandle))
	default:
		return firstNonEmpty(strings.TrimSpace(target.ContactURL), strings.TrimSpace(target.Website), strings.ToLower(strings.TrimSpace(target.Email)), strings.TrimSpace(target.Phone))
	}
}

func (h *Handlers) recordNotifyOutcomeAndMemory(
	ctx context.Context,
	subjectKind string,
	subjectRef string,
	targetID *int64,
	target *models.CaseEscalationTarget,
	preferredAssetClass string,
	outcomeType string,
	sourceType string,
	sourceRef string,
	note string,
	recordedAt time.Time,
) (*models.NotifyOutcome, *models.ContactEndpointMemory, error) {
	endpointKey := ""
	channelType := "email"
	channelValue := strings.ToLower(strings.TrimSpace(sourceRef))
	roleType := ""
	organizationKey := ""
	if target != nil {
		endpointKey = endpointKeyForTarget(*target)
		channelType = caseTargetChannel(*target)
		channelValue = endpointChannelValue(*target)
		roleType = target.RoleType
		organizationKey = target.OrganizationKey
	}
	if channelValue == "" {
		channelValue = strings.ToLower(strings.TrimSpace(sourceRef))
	}
	if endpointKey == "" && channelValue != "" {
		endpointKey = channelType + ":" + channelValue
	}
	evidenceJSON := fmt.Sprintf(
		`{"source_ref":"%s","note":"%s","channel":"%s"}`,
		escapeJSONString(sourceRef),
		escapeJSONString(strings.TrimSpace(note)),
		escapeJSONString(channelType),
	)
	outcome := &models.NotifyOutcome{
		SubjectKind:  subjectKind,
		SubjectRef:   subjectRef,
		TargetID:     targetID,
		EndpointKey:  endpointKey,
		OutcomeType:  outcomeType,
		SourceType:   sourceType,
		SourceRef:    sourceRef,
		EvidenceJSON: evidenceJSON,
		RecordedAt:   recordedAt,
	}
	if err := h.db.RecordNotifyOutcome(ctx, *outcome); err != nil {
		return nil, nil, err
	}
	if endpointKey == "" || channelValue == "" {
		return outcome, nil, nil
	}
	existingMemory, err := h.db.GetContactEndpointMemory(ctx, endpointKey)
	if err != nil {
		return nil, nil, err
	}
	memory := models.ContactEndpointMemory{
		EndpointKey:            endpointKey,
		OrganizationKey:        organizationKey,
		ChannelType:            emptyStringDefault(channelType, "email"),
		ChannelValue:           channelValue,
		LastResult:             outcomeType,
		PreferredForRoleType:   roleType,
		PreferredForAssetClass: strings.TrimSpace(preferredAssetClass),
		LastContactedAt:        &recordedAt,
		CooldownUntil:          notifyOutcomeCooldown(outcomeType, recordedAt),
	}
	if existingMemory != nil {
		memory.SuccessCount = existingMemory.SuccessCount
		memory.BounceCount = existingMemory.BounceCount
		memory.AckCount = existingMemory.AckCount
		memory.FixCount = existingMemory.FixCount
		memory.MisrouteCount = existingMemory.MisrouteCount
		memory.NoResponseCount = existingMemory.NoResponseCount
		if memory.OrganizationKey == "" {
			memory.OrganizationKey = existingMemory.OrganizationKey
		}
		if memory.PreferredForRoleType == "" {
			memory.PreferredForRoleType = existingMemory.PreferredForRoleType
		}
		if memory.PreferredForAssetClass == "" {
			memory.PreferredForAssetClass = existingMemory.PreferredForAssetClass
		}
	}
	switch outcomeType {
	case "sent":
		memory.SuccessCount++
	case "bounced":
		memory.BounceCount++
	case "acknowledged":
		memory.AckCount++
	case "fixed":
		memory.FixCount++
	case "misrouted":
		memory.MisrouteCount++
	case "no_response":
		memory.NoResponseCount++
	}
	savedMemory, err := h.db.UpsertContactEndpointMemory(ctx, memory)
	if err != nil {
		return nil, nil, err
	}
	return outcome, savedMemory, nil
}

func (h *Handlers) recordCaseExecutionOutcomeSideEffects(ctx context.Context, detail *models.CaseDetail, task *models.NotifyExecutionTask, target models.CaseEscalationTarget, outcomeType, actorUserID, note string) error {
	if detail == nil {
		return nil
	}
	summary := buildCaseExecutionOutcomeSummary(target, outcomeType, note)
	var linkedReportSeq *int
	if detail.Case.AnchorReportSeq != nil {
		linkedReportSeq = detail.Case.AnchorReportSeq
	}
	if err := h.db.InsertCaseResolutionSignal(ctx, detail.Case.CaseID, "notify_execution_task", summary, linkedReportSeq, map[string]any{
		"task_id":       task.ID,
		"target_id":     task.TargetID,
		"outcome_type":  outcomeType,
		"note":          note,
		"channel_type":  task.ChannelType,
		"execution_mode": task.ExecutionMode,
	}); err != nil {
		return err
	}
	return h.db.InsertCaseAuditEvent(ctx, detail.Case.CaseID, "execution_task_outcome_recorded", actorUserID, map[string]any{
		"task_id":      task.ID,
		"target_id":    task.TargetID,
		"outcome_type": outcomeType,
		"note":         note,
	})
}

func buildCaseExecutionOutcomeSummary(target models.CaseEscalationTarget, outcomeType, note string) string {
	label := firstNonEmpty(strings.TrimSpace(target.DisplayName), strings.TrimSpace(target.Organization), "contact")
	switch outcomeType {
	case "acknowledged":
		return fmt.Sprintf("%s acknowledged the escalation. %s", label, strings.TrimSpace(note))
	case "fixed":
		return fmt.Sprintf("%s reported the issue as fixed. %s", label, strings.TrimSpace(note))
	case "misrouted":
		return fmt.Sprintf("%s indicated the escalation was misrouted. %s", label, strings.TrimSpace(note))
	case "no_response":
		return fmt.Sprintf("No response recorded from %s. %s", label, strings.TrimSpace(note))
	default:
		return fmt.Sprintf("Execution outcome %s recorded for %s. %s", outcomeType, label, strings.TrimSpace(note))
	}
}

func findCaseTargetByID(targets []models.CaseEscalationTarget, targetID *int64) (models.CaseEscalationTarget, bool) {
	if targetID == nil {
		return models.CaseEscalationTarget{}, false
	}
	for _, target := range targets {
		if target.ID == *targetID {
			return target, true
		}
	}
	return models.CaseEscalationTarget{}, false
}

func parseTaskIDParam(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("missing task id")
	}
	var taskID int64
	if _, err := fmt.Sscanf(raw, "%d", &taskID); err != nil || taskID <= 0 {
		return 0, fmt.Errorf("invalid task id")
	}
	return taskID, nil
}

func (h *Handlers) sendCaseEscalation(c *gin.Context, caseID, body, subject string, targets []models.CaseEscalationTarget, ccRecipients []internalCaseEscalationRecipient) ([]internalCaseEscalationSendResult, error) {
	recipients := make([]internalCaseEscalationRecipient, 0, len(targets)+len(ccRecipients))
	for _, target := range targets {
		if strings.TrimSpace(target.Email) == "" {
			continue
		}
		targetID := target.ID
		recipients = append(recipients, internalCaseEscalationRecipient{
			TargetID:       &targetID,
			Email:          target.Email,
			DeliverySource: emptyStringDefault(target.TargetSource, "case_target"),
			DisplayName:    target.DisplayName,
			Organization:   target.Organization,
		})
	}
	recipients = append(recipients, ccRecipients...)
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no email recipients selected")
	}

	payload := internalCaseEscalationSendRequest{
		CaseID:     caseID,
		Subject:    subject,
		Body:       body,
		HTMLBody:   plainTextToHTML(body),
		Recipients: recipients,
	}
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, h.cfg.EmailServiceURL+"/internal/case-escalations/send", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Admin-Token", h.cfg.InternalAdminToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("email service returned http %d", resp.StatusCode)
	}
	var decoded internalCaseEscalationSendResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded.Results, nil
}

func buildCaseEscalationDraft(detail *models.CaseDetail, targets []models.CaseEscalationTarget, subject, body string) (string, string) {
	locale := inferCaseEscalationLocale(detail, targets)
	if strings.TrimSpace(subject) == "" {
		subject = buildCaseEscalationSubject(locale, detail.Case.Title)
	}
	if strings.TrimSpace(body) != "" {
		return strings.TrimSpace(subject), strings.TrimSpace(body)
	}

	return strings.TrimSpace(subject), strings.TrimSpace(buildCaseEscalationBody(locale, detail))
}

func plainTextToHTML(body string) string {
	escaped := html.EscapeString(body)
	escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	return "<p>" + escaped + "</p>"
}

func selectCaseTargets(targets []models.CaseEscalationTarget, targetIDs []int64) []models.CaseEscalationTarget {
	if len(targets) == 0 {
		return nil
	}
	allowAll := len(targetIDs) == 0
	allowed := make(map[int64]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		allowed[id] = struct{}{}
	}
	selected := make([]models.CaseEscalationTarget, 0, len(targets))
	for _, target := range targets {
		if normalizeEmail(target.Email) == "" {
			continue
		}
		if !allowAll {
			if _, ok := allowed[target.ID]; !ok {
				continue
			}
		}
		selected = append(selected, target)
	}
	return selected
}

func normalizeManualCCEmails(raw []string, selectedTargets []models.CaseEscalationTarget) []string {
	if len(raw) == 0 {
		return nil
	}
	excluded := make(map[string]struct{}, len(selectedTargets))
	for _, target := range selectedTargets {
		emailAddr := strings.ToLower(strings.TrimSpace(target.Email))
		if emailAddr == "" {
			continue
		}
		excluded[emailAddr] = struct{}{}
	}

	normalized := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		for _, part := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == ';' || unicode.IsSpace(r)
		}) {
			emailAddr := normalizeEmail(part)
			if emailAddr == "" {
				continue
			}
			if _, skip := excluded[emailAddr]; skip {
				continue
			}
			if _, ok := seen[emailAddr]; ok {
				continue
			}
			seen[emailAddr] = struct{}{}
			normalized = append(normalized, emailAddr)
		}
	}
	sort.Strings(normalized)
	return normalized
}

func buildManualCCRecipients(raw []string, selectedTargets []models.CaseEscalationTarget) []internalCaseEscalationRecipient {
	ccEmails := normalizeManualCCEmails(raw, selectedTargets)
	recipients := make([]internalCaseEscalationRecipient, 0, len(ccEmails))
	for _, emailAddr := range ccEmails {
		recipients = append(recipients, internalCaseEscalationRecipient{
			Email:          emailAddr,
			DeliverySource: "manual_cc",
		})
	}
	return recipients
}

func inferCaseEscalationLocale(detail *models.CaseDetail, targets []models.CaseEscalationTarget) string {
	languageSignals := make([]string, 0, len(targets)*3+2)
	languageSignals = append(languageSignals, detail.Case.Title, detail.Case.Summary)
	for _, target := range targets {
		languageSignals = append(languageSignals, target.DisplayName, target.Organization, target.Email)
	}
	corpus := strings.ToLower(strings.Join(languageSignals, " "))

	switch {
	case containsAny(corpus, []string{
		"schulhaus", "schule", "verwaltung", "sicherheit", "kopfholz", "adliswil",
		"zürich", "strasse", "@adliswil.ch", ".ch",
	}):
		return "de"
	case containsAny(corpus, []string{
		"mairie", "sécurité", "travaux publics", "commune", ".fr", "@ville", "école",
	}):
		return "fr"
	case containsAny(corpus, []string{
		"comune", "sicurezza", "scuola", "segnalazione", ".it",
	}):
		return "it"
	case containsAny(corpus, []string{
		"ayuntamiento", "seguridad", "escuela", "incidencia", ".es",
	}):
		return "es"
	case containsAny(corpus, []string{
		"prefeitura", "segurança", "escola", "ocorrência", ".pt", ".br",
	}):
		return "pt"
	default:
		return "en"
	}
}

func buildCaseEscalationSubject(locale, title string) string {
	title = localizeCaseTitle(locale, title)
	if title == "" {
		title = "incident cluster"
	}
	switch locale {
	case "de":
		return fmt.Sprintf("CleanApp-Meldung: %s", title)
	case "fr":
		return fmt.Sprintf("Alerte CleanApp : %s", title)
	case "it":
		return fmt.Sprintf("Segnalazione CleanApp: %s", title)
	case "es":
		return fmt.Sprintf("Alerta de CleanApp: %s", title)
	case "pt":
		return fmt.Sprintf("Alerta do CleanApp: %s", title)
	default:
		return fmt.Sprintf("CleanApp escalation: %s", title)
	}
}

func buildCaseEscalationBody(locale string, detail *models.CaseDetail) string {
	title := localizeCaseTitle(locale, detail.Case.Title)
	if title == "" {
		title = "incident cluster"
	}
	summary := localizeCaseSummary(locale, detail.Case.Summary)
	if summary == "" {
		summary = fallbackCaseSummary(detail)
	}
	severityPct := int(math.Round(detail.Case.SeverityScore * 100))
	urgencyPct := int(math.Round(detail.Case.UrgencyScore * 100))
	linkedCount := len(detail.LinkedReports)
	casePermalink := buildCasePermalink(detail.Case.CaseID)

	switch locale {
	case "de":
		return fmt.Sprintf(
			"Guten Tag,\n\nCleanApp hat den Fall „%s“ erfasst.\n\nKurzbeschreibung:\n%s\n\nSchweregrad: %d%%\nDringlichkeit: %d%%\nVerknüpfte Meldungen: %d\nFall-Link: %s\n\nBitte prüfen Sie den Standort und teilen Sie uns kurz mit, welche Schritte eingeleitet werden.\n\nFreundliche Grüsse\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	case "fr":
		return fmt.Sprintf(
			"Bonjour,\n\nCleanApp a enregistré le dossier « %s ».\n\nRésumé :\n%s\n\nGravité : %d%%\nUrgence : %d%%\nSignalements liés : %d\nLien du dossier : %s\n\nMerci de vérifier la situation et de nous indiquer brièvement les mesures prévues.\n\nCordialement,\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	case "it":
		return fmt.Sprintf(
			"Buongiorno,\n\nCleanApp ha registrato il caso \"%s\".\n\nSintesi:\n%s\n\nGravità: %d%%\nUrgenza: %d%%\nSegnalazioni collegate: %d\nLink del caso: %s\n\nVi chiediamo di verificare il sito e di indicarci brevemente quali azioni verranno intraprese.\n\nCordiali saluti,\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	case "es":
		return fmt.Sprintf(
			"Hola,\n\nCleanApp ha registrado el caso \"%s\".\n\nResumen:\n%s\n\nSeveridad: %d%%\nUrgencia: %d%%\nReportes vinculados: %d\nEnlace del caso: %s\n\nPor favor, revisen la situación y confírmennos brevemente qué acciones van a tomar.\n\nSaludos,\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	case "pt":
		return fmt.Sprintf(
			"Olá,\n\nO CleanApp registou o caso \"%s\".\n\nResumo:\n%s\n\nSeveridade: %d%%\nUrgência: %d%%\nRelatórios associados: %d\nLink do caso: %s\n\nPedimos que verifiquem a situação e nos indiquem brevemente quais medidas serão tomadas.\n\nCumprimentos,\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	default:
		return fmt.Sprintf(
			"Hello,\n\nCleanApp has opened the case \"%s\".\n\nSummary:\n%s\n\nSeverity: %d%%\nUrgency: %d%%\nLinked reports: %d\nCase link: %s\n\nPlease review the location and let us know what remediation steps will be taken.\n\nBest,\nCleanApp",
			title,
			summary,
			severityPct,
			urgencyPct,
			linkedCount,
			casePermalink,
		)
	}
}

func fallbackCaseSummary(detail *models.CaseDetail) string {
	reports := append([]models.CaseReportLink(nil), detail.LinkedReports...)
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].SeverityLevel == reports[j].SeverityLevel {
			return reports[i].ReportTimestamp.After(reports[j].ReportTimestamp)
		}
		return reports[i].SeverityLevel > reports[j].SeverityLevel
	})
	if len(reports) == 0 {
		return "A cluster of incident reports has been linked to this case."
	}
	for _, report := range reports {
		line := strings.TrimSpace(report.Title)
		if line == "" {
			line = strings.TrimSpace(report.Summary)
		}
		if line != "" {
			return line
		}
	}
	return fmt.Sprintf("A cluster of %d incident reports has been linked to this case.", len(reports))
}

func containsAny(corpus string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(corpus, needle) {
			return true
		}
	}
	return false
}

func localizeCaseTitle(locale, title string) string {
	trimmed := strings.TrimSpace(title)
	lower := strings.ToLower(trimmed)
	const prefix = "incident cluster at "
	if !strings.HasPrefix(lower, prefix) {
		return trimmed
	}
	landmark := strings.TrimSpace(trimmed[len(prefix):])
	if landmark == "" {
		return trimmed
	}
	switch locale {
	case "de":
		return fmt.Sprintf("Vorfallcluster bei %s", landmark)
	case "fr":
		return fmt.Sprintf("Groupe d'incidents à %s", landmark)
	case "it":
		return fmt.Sprintf("Cluster di incidenti presso %s", landmark)
	case "es":
		return fmt.Sprintf("Clúster de incidentes en %s", landmark)
	case "pt":
		return fmt.Sprintf("Cluster de incidentes em %s", landmark)
	default:
		return trimmed
	}
}

func localizeCaseSummary(locale, summary string) string {
	trimmed := strings.TrimSpace(summary)
	lower := strings.ToLower(trimmed)
	const prefix = "case created from area scope around "
	if !strings.HasPrefix(lower, prefix) {
		return trimmed
	}
	landmark := strings.TrimSpace(strings.TrimSuffix(trimmed[len(prefix):], "."))
	if landmark == "" {
		return trimmed
	}
	switch locale {
	case "de":
		return fmt.Sprintf("Der Fall wurde aus einem Bereich rund um %s erstellt.", landmark)
	case "fr":
		return fmt.Sprintf("Le dossier a été créé à partir d'une zone autour de %s.", landmark)
	case "it":
		return fmt.Sprintf("Il caso è stato creato a partire da un'area intorno a %s.", landmark)
	case "es":
		return fmt.Sprintf("El caso se creó a partir de un área alrededor de %s.", landmark)
	case "pt":
		return fmt.Sprintf("O caso foi criado a partir de uma área em torno de %s.", landmark)
	default:
		return trimmed
	}
}

func buildCasePermalink(caseID string) string {
	return fmt.Sprintf("https://www.cleanapp.io/cases/%s", strings.TrimSpace(caseID))
}

func emptyStringDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isEmptyJSONBodyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "EOF")
}
