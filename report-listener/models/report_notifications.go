package models

import "time"

// A feed row counts recorded sent emails for its canonical report only. It
// never contains recipient/contact/provider metadata; unknown is not zero.
type PublicNotificationCount struct {
	State      string `json:"state"`
	EmailCount *int   `json:"email_count,omitempty"`
}

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
	// Timeline is what came back after sending, oldest first. Always a list
	// (possibly empty); free for everyone. See ReportOutcomeEvent.
	Timeline []ReportOutcomeEvent `json:"timeline"`
}

// ReportOutcomeEvent is one recorded outcome of notifying someone about a
// report: viewed, acknowledged, replied, bounced, no_response, fixed or
// out_of_office. It carries no message text, sender or recipient. Count is
// how many times the same outcome was recorded; At is the first time.
type ReportOutcomeEvent struct {
	Type      string    `json:"type"`
	At        time.Time `json:"at"`
	Count     int       `json:"count"`
	TicketRef string    `json:"ticket_ref,omitempty"`
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
