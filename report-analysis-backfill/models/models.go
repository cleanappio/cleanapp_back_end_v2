package models

import "time"

// Report represents a report from the reports table
type Report struct {
	Seq         int       `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	ID          string    `json:"id"`
	Team        int       `json:"team"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	Image       []byte    `json:"image"`
	ActionID    string    `json:"action_id"`
	Description string    `json:"description"`
}
