package models

// ReportWithTagsMessage represents a message for adding tags to a report via RabbitMQ
// This matches the structure expected by the report-tags service subscriber
type ReportWithTagsMessage struct {
	Seq  int      `json:"seq"`
	Tags []string `json:"tags"`
}
