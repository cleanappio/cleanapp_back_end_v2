package models

import "time"

// ClientAuth represents a user in the authentication system
type ClientAuth struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LoginMethod represents an authentication method for a user
type LoginMethod struct {
	ID         int       `json:"id"`
	UserID     string    `json:"user_id"`
	MethodType string    `json:"method_type"`
	OAuthID    string    `json:"oauth_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateUserRequest represents the request to create a new user
type CreateUserRequest struct {
	Name     string `json:"name" binding:"required,max=256"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

// UpdateUserRequest represents the request to update user information
type UpdateUserRequest struct {
	Name  *string `json:"name,omitempty" binding:"omitempty,max=256"`
	Email *string `json:"email,omitempty" binding:"omitempty,email"`
}

// LoginRequest represents the authentication request
// For email/password login: provide email and password
// For OAuth login: provide provider and token
type LoginRequest struct {
	Email    string `json:"email" binding:"required_without=Provider"`
	Password string `json:"password" binding:"required_without=Provider"`
	Provider string `json:"provider" binding:"required_without=Email,omitempty,oneof=google apple facebook"`
	Token    string `json:"token" binding:"required_with=Provider"` // OAuth ID from provider
}

// TokenResponse represents the authentication response
type TokenResponse struct {
	Token        string      `json:"token"`
	RefreshToken string      `json:"refresh_token,omitempty"`
	TokenType    string      `json:"token_type,omitempty"`
	ExpiresIn    int         `json:"expires_in,omitempty"`
	User         *ClientAuth `json:"user,omitempty"` // Only for new OAuth registrations
}

// OAuthLoginRequest represents an OAuth authentication request
type OAuthLoginRequest struct {
	Provider          string                 `json:"provider" binding:"required,oneof=google facebook apple"`
	IDToken           string                 `json:"id_token,omitempty"`
	AccessToken       string                 `json:"access_token,omitempty"`
	AuthorizationCode string                 `json:"authorization_code,omitempty"`
	UserInfo          map[string]interface{} `json:"user_info,omitempty"`
}

// OAuthURLResponse represents the OAuth URL response
type OAuthURLResponse struct {
	URL   string `json:"url"`
	State string `json:"state,omitempty"`
}

// OAuthUserInfo represents user information from OAuth provider
type OAuthUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

// MessageResponse represents a simple message response
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// UserExistsResponse represents the response for user existence check
type UserExistsResponse struct {
	UserExists bool `json:"user_exists"`
}

// ValidateTokenRequest represents a token validation request from other services
type ValidateTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// ValidateTokenResponse represents a token validation response
type ValidateTokenResponse struct {
	Valid  bool   `json:"valid"`
	UserID string `json:"user_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RefreshTokenRequest represents a token refresh request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

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
