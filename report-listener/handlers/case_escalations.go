package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"

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
	c.JSON(http.StatusOK, gin.H{
		"case_id":      detail.Case.CaseID,
		"targets":      detail.EscalationTargets,
		"actions":      detail.EscalationActions,
		"deliveries":   detail.EmailDeliveries,
		"linked_count": len(detail.LinkedReports),
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
	subject, body := buildCaseEscalationDraft(detail, req.Subject, req.Body)
	c.JSON(http.StatusOK, models.CaseEscalationDraftResponse{
		CaseID:      detail.Case.CaseID,
		Subject:     subject,
		Body:        body,
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

	subject, body := buildCaseEscalationDraft(detail, req.Subject, req.Body)
	actorUserID := middleware.GetUserIDFromContext(c)
	if strings.TrimSpace(req.ActorUserID) != "" {
		actorUserID = req.ActorUserID
	}

	selectedTargets, actions, err := h.db.CreateCaseEscalationActions(c.Request.Context(), detail.Case.CaseID, req.TargetIDs, subject, body, actorUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create escalation actions"})
		return
	}

	sendResults, err := h.sendCaseEscalation(c, detail.Case.CaseID, body, subject, selectedTargets)
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

	c.JSON(http.StatusOK, models.CaseEscalationSendResponse{
		CaseID:     detail.Case.CaseID,
		Subject:    subject,
		Body:       body,
		Actions:    actions,
		Deliveries: deliveries,
	})
}

func (h *Handlers) sendCaseEscalation(c *gin.Context, caseID, body, subject string, targets []models.CaseEscalationTarget) ([]internalCaseEscalationSendResult, error) {
	recipients := make([]internalCaseEscalationRecipient, 0, len(targets))
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

func buildCaseEscalationDraft(detail *models.CaseDetail, subject, body string) (string, string) {
	if strings.TrimSpace(subject) == "" {
		subject = fmt.Sprintf("CleanApp escalation: %s", detail.Case.Title)
	}
	if strings.TrimSpace(body) != "" {
		return strings.TrimSpace(subject), strings.TrimSpace(body)
	}

	reports := append([]models.CaseReportLink(nil), detail.LinkedReports...)
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].SeverityLevel == reports[j].SeverityLevel {
			return reports[i].ReportTimestamp.After(reports[j].ReportTimestamp)
		}
		return reports[i].SeverityLevel > reports[j].SeverityLevel
	})

	var b strings.Builder
	fmt.Fprintf(&b, "CleanApp has opened case %s (%s).\n\n", detail.Case.Title, detail.Case.CaseID)
	if strings.TrimSpace(detail.Case.Summary) != "" {
		fmt.Fprintf(&b, "Summary:\n%s\n\n", strings.TrimSpace(detail.Case.Summary))
	}
	fmt.Fprintf(&b, "Classification: %s\n", detail.Case.Classification)
	fmt.Fprintf(&b, "Status: %s\n", detail.Case.Status)
	fmt.Fprintf(&b, "Linked reports: %d\n\n", len(detail.LinkedReports))
	if len(reports) > 0 {
		b.WriteString("Representative reports:\n")
		limit := 3
		if len(reports) < limit {
			limit = len(reports)
		}
		for i := 0; i < limit; i++ {
			report := reports[i]
			line := strings.TrimSpace(report.Title)
			if line == "" {
				line = strings.TrimSpace(report.Summary)
			}
			if line == "" {
				line = fmt.Sprintf("Report #%d", report.Seq)
			}
			fmt.Fprintf(&b, "- Report #%d (%s, sev %.2f): %s\n",
				report.Seq,
				report.ReportTimestamp.UTC().Format(time.RFC3339),
				report.SeverityLevel,
				line,
			)
		}
		b.WriteString("\n")
	}
	b.WriteString("Please review and advise on remediation steps.\n")
	return strings.TrimSpace(subject), strings.TrimSpace(b.String())
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
		if strings.TrimSpace(target.Email) == "" {
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
