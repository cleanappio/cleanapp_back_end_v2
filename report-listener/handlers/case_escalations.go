package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
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

	c.JSON(http.StatusOK, models.CaseEscalationSendResponse{
		CaseID:     detail.Case.CaseID,
		Subject:    subject,
		Body:       body,
		CCEmails:   ccEmails,
		Actions:    actions,
		Deliveries: deliveries,
	})
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
