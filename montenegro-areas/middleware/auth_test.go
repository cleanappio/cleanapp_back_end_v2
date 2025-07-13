package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"montenegro-areas/config"
)

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name        string
		authHeader  string
		expected    string
		shouldError bool
	}{
		{
			name:        "valid bearer token",
			authHeader:  "Bearer test-token-123",
			expected:    "test-token-123",
			shouldError: false,
		},
		{
			name:        "missing bearer prefix",
			authHeader:  "test-token-123",
			expected:    "",
			shouldError: true,
		},
		{
			name:        "empty header",
			authHeader:  "",
			expected:    "",
			shouldError: true,
		},
		{
			name:        "bearer with empty token",
			authHeader:  "Bearer ",
			expected:    "",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToken(tt.authHeader)
			if tt.shouldError && result != "" {
				t.Errorf("expected empty token for invalid header, got %s", result)
			}
			if !tt.shouldError && result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	cfg := &config.Config{
		AuthServiceURL: "http://localhost:8080",
	}

	middleware := AuthMiddleware(cfg)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "missing authorization header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid authorization format",
			authHeader:     "InvalidFormat token123",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "valid bearer format",
			authHeader:     "Bearer test-token",
			expectedStatus: http.StatusUnauthorized, // Will fail because auth-service is not running
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware(handler).ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectedStatus)
			}
		})
	}
}

func TestGetUserIDFromContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	// Test with no user ID in context
	userID := GetUserIDFromContext(req)
	if userID != "" {
		t.Errorf("expected empty user ID, got %s", userID)
	}

	// Test with user ID in context
	ctx := req.Context()
	ctx = context.WithValue(ctx, "user_id", "test-user-123")
	req = req.WithContext(ctx)

	userID = GetUserIDFromContext(req)
	if userID != "test-user-123" {
		t.Errorf("expected test-user-123, got %s", userID)
	}
}

func TestGetTokenFromContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	// Test with no token in context
	token := GetTokenFromContext(req)
	if token != "" {
		t.Errorf("expected empty token, got %s", token)
	}

	// Test with token in context
	ctx := req.Context()
	ctx = context.WithValue(ctx, "token", "test-token-123")
	req = req.WithContext(ctx)

	token = GetTokenFromContext(req)
	if token != "test-token-123" {
		t.Errorf("expected test-token-123, got %s", token)
	}
}
