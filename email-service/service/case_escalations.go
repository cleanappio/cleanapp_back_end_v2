package service

import (
	"context"
	"strings"
	"time"
)

type CaseEscalationRecipient struct {
	TargetID       *int64 `json:"target_id,omitempty"`
	Email          string `json:"email"`
	DeliverySource string `json:"delivery_source"`
	DisplayName    string `json:"display_name,omitempty"`
	Organization   string `json:"organization,omitempty"`
}

type CaseEscalationSendResult struct {
	TargetID          *int64     `json:"target_id,omitempty"`
	Email             string     `json:"email"`
	Status            string     `json:"status"`
	DeliverySource    string     `json:"delivery_source"`
	Provider          string     `json:"provider"`
	ProviderMessageID string     `json:"provider_message_id,omitempty"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	Error             string     `json:"error,omitempty"`
}

func (s *EmailService) SendCaseEscalationEmails(
	ctx context.Context,
	subject string,
	body string,
	htmlBody string,
	recipients []CaseEscalationRecipient,
) []CaseEscalationSendResult {
	results := make([]CaseEscalationSendResult, 0, len(recipients))
	for _, recipient := range recipients {
		emailAddr := strings.ToLower(strings.TrimSpace(recipient.Email))
		result := CaseEscalationSendResult{
			TargetID:       recipient.TargetID,
			Email:          emailAddr,
			DeliverySource: strings.TrimSpace(recipient.DeliverySource),
			Provider:       "sendgrid",
		}
		if result.DeliverySource == "" {
			result.DeliverySource = "case_target"
		}
		if !s.isValidEmail(emailAddr) {
			result.Status = "invalid_email"
			result.Error = "invalid recipient email"
			results = append(results, result)
			continue
		}
		optedOut, err := s.isEmailOptedOut(ctx, emailAddr)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		if optedOut {
			result.Status = "opted_out"
			result.Error = "recipient opted out"
			results = append(results, result)
			continue
		}

		sendResults := s.email.SendCustomEmails([]string{emailAddr}, subject, body, htmlBody)
		if len(sendResults) == 0 {
			result.Status = "failed"
			result.Error = "no send result returned"
			results = append(results, result)
			continue
		}
		sendResult := sendResults[0]
		result.Status = sendResult.Status
		result.Provider = sendResult.Provider
		result.ProviderMessageID = sendResult.ProviderMessageID
		if !sendResult.SentAt.IsZero() {
			sentAt := sendResult.SentAt.UTC()
			result.SentAt = &sentAt
		}
		result.Error = sendResult.Error
		if sendResult.Status == "sent" {
			if err := s.recordEmailSent(ctx, emailAddr); err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				result.ProviderMessageID = ""
				result.SentAt = nil
			}
		}
		results = append(results, result)
	}
	return results
}
