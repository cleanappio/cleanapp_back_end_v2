package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"report-listener/mobilepush"
	"report-listener/models"
)

type mobilePushRegisterRequest struct {
	InstallID            string `json:"install_id"`
	Platform             string `json:"platform"`
	Provider             string `json:"provider"`
	PushToken            string `json:"push_token"`
	AppVersion           string `json:"app_version,omitempty"`
	NotificationsEnabled bool   `json:"notifications_enabled"`
}

type mobilePushUnregisterRequest struct {
	InstallID string `json:"install_id"`
	Provider  string `json:"provider"`
}

type reportPushDeliveryRequest struct {
	Seq            int    `json:"seq"`
	Status         string `json:"status"`
	RecipientCount int    `json:"recipient_count"`
}

func normalizePushProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fcm":
		return "fcm"
	default:
		return "apns"
	}
}

func normalizePushPlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "android":
		return "android"
	default:
		return "ios"
	}
}

func (h *Handlers) RegisterMobilePushDevice(c *gin.Context) {
	var req mobilePushRegisterRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	req.InstallID = strings.TrimSpace(req.InstallID)
	req.Provider = normalizePushProvider(req.Provider)
	req.Platform = normalizePushPlatform(req.Platform)
	req.PushToken = strings.TrimSpace(req.PushToken)
	if req.InstallID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "install_id is required"})
		return
	}
	if req.NotificationsEnabled && req.PushToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "push_token is required"})
		return
	}

	if err := h.db.UpsertMobilePushDevice(c.Request.Context(), models.MobilePushDevice{
		InstallID:            req.InstallID,
		Platform:             req.Platform,
		Provider:             req.Provider,
		PushToken:            req.PushToken,
		AppVersion:           strings.TrimSpace(req.AppVersion),
		NotificationsEnabled: req.NotificationsEnabled,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register push device"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":                    true,
		"install_id":            req.InstallID,
		"provider":              req.Provider,
		"notifications_enabled": req.NotificationsEnabled,
	})
}

func (h *Handlers) UnregisterMobilePushDevice(c *gin.Context) {
	var req mobilePushUnregisterRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if err := h.db.DeactivateMobilePushDevice(c.Request.Context(), req.InstallID, normalizePushProvider(req.Provider)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unregister push device"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func buildReportDeliveryPushMessage(status string, recipientCount int, recipients []models.ReportDeliveryRecipient) (string, string) {
	switch status {
	case "sent":
		if len(recipients) > 0 {
			primary := recipients[0]
			label := firstNonEmpty(strings.TrimSpace(primary.DisplayName), strings.TrimSpace(primary.Organization), strings.TrimSpace(primary.Email))
			email := strings.TrimSpace(primary.Email)
			sentAt := formatPushTimestamp(primary.SentAt)
			switch {
			case recipientCount > 1 && label != "" && email != "":
				return "Report sent", "Your report was sent to " + label + " at " + email + formatPushTimestampSuffix(sentAt) + " and " + strconv.Itoa(recipientCount-1) + " more recipient(s)."
			case recipientCount > 1 && email != "":
				return "Report sent", "Your report was sent to " + email + formatPushTimestampSuffix(sentAt) + " and " + strconv.Itoa(recipientCount-1) + " more recipient(s)."
			case label != "" && email != "":
				return "Report sent", "Your report was sent to " + label + " at " + email + formatPushTimestampSuffix(sentAt) + "."
			case email != "":
				return "Report sent", "Your report was sent to " + email + formatPushTimestampSuffix(sentAt) + "."
			case label != "":
				return "Report sent", "Your report was sent to " + label + formatPushTimestampSuffix(sentAt) + "."
			}
		}
		if recipientCount == 1 {
			return "Report sent", "Your report was sent to 1 responsible party."
		}
		if recipientCount > 1 {
			return "Report sent", "Your report was sent to " + strconv.Itoa(recipientCount) + " responsible parties."
		}
		return "Report sent", "Your report was sent to the responsible party."
	case "processed_no_delivery":
		return "Report processed", "Your report was processed, but no responsible party could be contacted yet."
	default:
		return "Report update", "Your report status changed."
	}
}

func (h *Handlers) PushReportDeliveryUpdate(c *gin.Context) {
	var req reportPushDeliveryRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.Seq <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "seq is required"})
		return
	}
	if req.Status != "sent" && req.Status != "processed_no_delivery" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported status"})
		return
	}

	sent, skipped, err := h.dispatchReportDeliveryPush(c.Request.Context(), req.Seq, req.Status, req.RecipientCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to dispatch report delivery push"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"sent":    sent,
		"skipped": skipped,
	})
}

func (h *Handlers) dispatchReportDeliveryPush(ctx context.Context, seq int, status string, recipientCount int) (int, int, error) {
	devices, err := h.db.GetReportPushDevices(ctx, seq)
	if err != nil {
		return 0, 0, err
	}
	if len(devices) == 0 {
		return 0, 0, nil
	}

	publicID := ""
	if report, err := h.db.GetReportBySeq(ctx, seq); err == nil && report != nil {
		publicID = strings.TrimSpace(report.Report.PublicID)
	}

	recipients, err := h.db.ListReportDeliveryRecipients(ctx, seq)
	if err != nil {
		log.Printf("warn: failed to load report delivery recipients for push seq %d: %v", seq, err)
	}

	title, body := buildReportDeliveryPushMessage(status, recipientCount, recipients)
	message := map[string]string{
		"seq":    strconv.Itoa(seq),
		"status": status,
	}
	if publicID != "" {
		message["public_id"] = publicID
	}
	if len(recipients) > 0 {
		primary := recipients[0]
		if primary.Email != "" {
			message["recipient_email"] = primary.Email
		}
		if primary.DisplayName != "" {
			message["recipient_name"] = primary.DisplayName
		} else if primary.Organization != "" {
			message["recipient_name"] = primary.Organization
		}
		if primary.SentAt != nil {
			message["sent_at"] = primary.SentAt.UTC().Format(time.RFC3339)
		}
	}
	message["navigate_to"] = "my_report_details"

	sent := 0
	skipped := 0
	for _, device := range devices {
		alreadySent, err := h.db.HasMobilePushDeliveryEvent(ctx, seq, device.InstallID, status)
		if err != nil {
			return sent, skipped, err
		}
		if alreadySent {
			skipped++
			continue
		}

		result, sendErr := h.pushSender.Send(ctx, device.Provider, device.PushToken, mobilepush.Message{
			Title: title,
			Body:  body,
			Data:  message,
		})
		if result.Disabled {
			log.Printf("warn: mobile push skipped for report %d install %s provider %s: %s", seq, device.InstallID, device.Provider, result.ResponseBody)
			skipped++
			continue
		}
		if result.InvalidDevice {
			_ = h.db.DeactivateMobilePushDeviceByID(ctx, device.ID)
		}

		recordErr := h.db.RecordMobilePushDeliveryEvent(ctx, models.ReportPushDeliveryEvent{
			ReportSeq:      seq,
			InstallID:      device.InstallID,
			DeliveryStatus: status,
			Provider:       result.Provider,
			ResponseCode:   result.StatusCode,
			ResponseBody:   result.ResponseBody,
		})
		if recordErr != nil {
			return sent, skipped, recordErr
		}

		if sendErr != nil {
			log.Printf("warn: mobile push send failed for report %d install %s provider %s: %v (status=%d body=%s)", seq, device.InstallID, device.Provider, sendErr, result.StatusCode, strings.TrimSpace(result.ResponseBody))
			skipped++
			continue
		}
		sent++
	}

	return sent, skipped, nil
}

func formatPushTimestamp(sentAt *time.Time) string {
	if sentAt == nil {
		return ""
	}
	return sentAt.UTC().Format("2006-01-02 15:04 UTC")
}

func formatPushTimestampSuffix(formatted string) string {
	if strings.TrimSpace(formatted) == "" {
		return ""
	}
	return " on " + formatted
}
