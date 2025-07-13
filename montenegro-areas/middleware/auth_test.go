package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"montenegro-areas/config"

	"github.com/gin-gonic/gin"
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
	gin.SetMode(gin.TestMode)
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
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				c.Request.Header.Set("Authorization", tt.authHeader)
			}

			// Call middleware
			middleware(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestGetUserIDFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test with no user ID in context
	userID := GetUserIDFromContext(c)
	if userID != "" {
		t.Errorf("expected empty user ID, got %s", userID)
	}

	// Test with user ID in context
	c.Set("user_id", "test-user-123")
	userID = GetUserIDFromContext(c)
	if userID != "test-user-123" {
		t.Errorf("expected test-user-123, got %s", userID)
	}
}

func TestGetTokenFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test with no token in context
	token := GetTokenFromContext(c)
	if token != "" {
		t.Errorf("expected empty token, got %s", token)
	}

	// Test with token in context
	c.Set("token", "test-token-123")
	token = GetTokenFromContext(c)
	if token != "test-token-123" {
		t.Errorf("expected test-token-123, got %s", token)
	}
}
