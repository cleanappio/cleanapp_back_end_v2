package models

import (
	"time"
)

// Report represents a report from the reports table
type Report struct {
	Seq       int       `json:"seq" db:"seq"`
	Timestamp time.Time `json:"timestamp" db:"ts"`
	ID        string    `json:"id" db:"id"`
	Team      int       `json:"team" db:"team"`
	Latitude  float64   `json:"latitude" db:"latitude"`
	Longitude float64   `json:"longitude" db:"longitude"`
	X         *float64  `json:"x,omitempty" db:"x"`
	Y         *float64  `json:"y,omitempty" db:"y"`
	Image     []byte    `json:"image" db:"image"`
	ActionID  *string   `json:"action_id,omitempty" db:"action_id"`
}

// ReportBatch represents a batch of reports to be broadcasted
type ReportBatch struct {
	Reports []Report `json:"reports"`
	Count   int      `json:"count"`
	FromSeq int      `json:"from_seq"`
	ToSeq   int      `json:"to_seq"`
}

// BroadcastMessage represents a message sent to WebSocket clients
type BroadcastMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status           string `json:"status"`
	Service          string `json:"service"`
	Timestamp        string `json:"timestamp"`
	ConnectedClients int    `json:"connected_clients"`
	LastBroadcastSeq int    `json:"last_broadcast_seq"`
}
