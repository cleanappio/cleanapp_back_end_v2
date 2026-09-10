package models

import "time"

type ReportSortMetrics struct {
	ReportSeq      int        `json:"report_seq"`
	SortCount      int        `json:"sort_count"`
	HighValueCount int        `json:"high_value_count"`
	SpamCount      int        `json:"spam_count"`
	UrgencySum     int        `json:"urgency_sum"`
	UrgencyMean    float64    `json:"urgency_mean"`
	LastSortedAt   *time.Time `json:"last_sorted_at,omitempty"`
}

type SortableReport struct {
	Report      Report            `json:"report"`
	SortMetrics ReportSortMetrics `json:"sort_metrics"`
}

type ReportSortVote struct {
	SorterID     string    `json:"sorter_id"`
	ReportSeq    int       `json:"report_seq"`
	Verdict      string    `json:"verdict"`
	UrgencyScore int       `json:"urgency_score"`
	CreatedAt    time.Time `json:"created_at"`
}

type ReportSortSubmissionResult struct {
	ReportSeq    int               `json:"report_seq"`
	Verdict      string            `json:"verdict"`
	UrgencyScore int               `json:"urgency_score"`
	RewardKITNs  int               `json:"reward_kitns"`
	SortMetrics  ReportSortMetrics `json:"sort_metrics"`
}
