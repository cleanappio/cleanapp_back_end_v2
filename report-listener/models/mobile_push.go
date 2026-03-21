package models

import "time"

type MobilePushDevice struct {
	ID                   int64     `json:"id"`
	InstallID            string    `json:"install_id"`
	Platform             string    `json:"platform"`
	Provider             string    `json:"provider"`
	PushToken            string    `json:"-"`
	PushTokenHash        string    `json:"push_token_hash"`
	AppVersion           string    `json:"app_version,omitempty"`
	NotificationsEnabled bool      `json:"notifications_enabled"`
	Status               string    `json:"status"`
	LastSeenAt           time.Time `json:"last_seen_at"`
}

type ReportPushDeliveryEvent struct {
	ReportSeq      int       `json:"report_seq"`
	InstallID      string    `json:"install_id"`
	DeliveryStatus string    `json:"delivery_status"`
	Provider       string    `json:"provider,omitempty"`
	ResponseCode   int       `json:"response_code,omitempty"`
	ResponseBody   string    `json:"response_body,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

type ReportDeliveryRecipient struct {
	Email          string     `json:"email"`
	DeliverySource string     `json:"delivery_source,omitempty"`
	DeliveryStatus string     `json:"delivery_status,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	DisplayName    string     `json:"display_name,omitempty"`
	Organization   string     `json:"organization,omitempty"`
}
