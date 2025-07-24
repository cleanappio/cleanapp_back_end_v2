package models

import (
	"time"
)

// ReportStatus represents a report status entry
type ReportStatus struct {
	Seq       int       `json:"seq" db:"seq"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// MarkResolvedRequest represents the request to mark a report as resolved
type MarkResolvedRequest struct {
	Seq int `json:"seq" binding:"required"`
}

// MarkResolvedResponse represents the response for marking a report as resolved
type MarkResolvedResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Seq     int    `json:"seq"`
	Status  string `json:"status"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Timestamp string `json:"timestamp"`
}
