package models

// ReportAuthorizationRequest represents a request to check report authorization
type ReportAuthorizationRequest struct {
	ReportSeqs []int `json:"report_seqs" binding:"required,min=1"`
}

// ReportAuthorization represents the authorization status for a single report
type ReportAuthorization struct {
	ReportSeq  int    `json:"report_seq"`
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason,omitempty"`
}

// ReportAuthorizationResponse represents the response for report authorization check
type ReportAuthorizationResponse struct {
	Authorizations []ReportAuthorization `json:"authorizations"`
}

// MessageResponse represents a simple message response
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}
