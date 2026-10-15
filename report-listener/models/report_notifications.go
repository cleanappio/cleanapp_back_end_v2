package models

import "time"

// Notification summaries deliberately contain no transport/provider metadata.
// Sent means accepted for sending, not delivered, read, or acknowledged.
type ReportNotificationEvent struct {
	SentAt      *time.Time `json:"sent_at"`
	EmailCount  int        `json:"email_count"`
	ContentKind string     `json:"content_kind"`
}

type ReportNotificationSummary struct {
	State                       string                    `json:"state"`
	EmailCount                  int                       `json:"email_count"`
	RecipientCount              int                       `json:"recipient_count"`
	LatestSentAt                *time.Time                `json:"latest_sent_at"`
	PreviewRecipients           []string                  `json:"preview_recipients"`
	HiddenRecipientCount        int                       `json:"hidden_recipient_count"`
	HistoryComplete             bool                      `json:"history_complete"`
	FullLogRequiresSubscription bool                      `json:"full_log_requires_subscription"`
	ContentKind                 string                    `json:"content_kind"`
	Events                      []ReportNotificationEvent `json:"events"`
}

type ReportNotificationRecipient struct {
	Email       string     `json:"email"`
	SentAt      *time.Time `json:"sent_at"`
	Status      string     `json:"status"`
	ContentKind string     `json:"content_kind"`
}

type ReportNotificationHistory struct {
	ReportNotificationSummary
	Recipients []ReportNotificationRecipient `json:"recipients"`
}
