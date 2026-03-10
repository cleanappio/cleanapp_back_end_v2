package handlers

import (
	"net/http"
	"strings"

	"email-service/service"

	"github.com/gin-gonic/gin"
)

type CaseEscalationSendRecipientRequest struct {
	TargetID       *int64 `json:"target_id,omitempty"`
	Email          string `json:"email" binding:"required"`
	DeliverySource string `json:"delivery_source"`
	DisplayName    string `json:"display_name,omitempty"`
	Organization   string `json:"organization,omitempty"`
}

type CaseEscalationSendRequest struct {
	CaseID     string                               `json:"case_id" binding:"required"`
	Subject    string                               `json:"subject" binding:"required"`
	Body       string                               `json:"body" binding:"required"`
	HTMLBody   string                               `json:"html_body,omitempty"`
	Recipients []CaseEscalationSendRecipientRequest `json:"recipients" binding:"required"`
}

type CaseEscalationSendResponse struct {
	CaseID  string                             `json:"case_id"`
	Results []service.CaseEscalationSendResult `json:"results"`
}

func (h *EmailServiceHandler) HandleSendCaseEscalation(c *gin.Context) {
	var req CaseEscalationSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(req.Recipients) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one recipient is required"})
		return
	}

	recipients := make([]service.CaseEscalationRecipient, 0, len(req.Recipients))
	for _, recipient := range req.Recipients {
		emailAddr := strings.TrimSpace(recipient.Email)
		if emailAddr == "" {
			continue
		}
		recipients = append(recipients, service.CaseEscalationRecipient{
			TargetID:       recipient.TargetID,
			Email:          emailAddr,
			DeliverySource: recipient.DeliverySource,
			DisplayName:    recipient.DisplayName,
			Organization:   recipient.Organization,
		})
	}
	if len(recipients) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid recipients provided"})
		return
	}

	results := h.emailService.SendCaseEscalationEmails(c.Request.Context(), req.Subject, req.Body, req.HTMLBody, recipients)
	c.JSON(http.StatusOK, CaseEscalationSendResponse{
		CaseID:  req.CaseID,
		Results: results,
	})
}
