package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom-area-dashboard/config"

	"github.com/gin-gonic/gin"
)

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name        string
		authHeader  string
		expected    string
		shouldError bool
	}{
		{name: "valid bearer token", authHeader: "Bearer test-token-123", expected: "test-token-123"},
		{name: "missing bearer prefix", authHeader: "test-token-123", shouldError: true},
		{name: "empty header", authHeader: "", shouldError: true},
		{name: "bearer with empty token", authHeader: "Bearer ", shouldError: true},
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
	cfg := &config.Config{JWTSecret: "dev-jwt-secret"}

	middleware := AuthMiddleware(cfg, (*sql.DB)(nil))

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{name: "missing authorization header", expectedStatus: http.StatusUnauthorized},
		{name: "invalid authorization format", authHeader: "InvalidFormat token123", expectedStatus: http.StatusUnauthorized},
		{name: "valid bearer format", authHeader: "Bearer test-token", expectedStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				c.Request.Header.Set("Authorization", tt.authHeader)
			}

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
	if userID := GetUserIDFromContext(c); userID != "" {
		t.Errorf("expected empty user ID, got %s", userID)
	}
	c.Set("user_id", "test-user-123")
	if userID := GetUserIDFromContext(c); userID != "test-user-123" {
		t.Errorf("expected test-user-123, got %s", userID)
	}
}

func TestGetTokenFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if token := GetTokenFromContext(c); token != "" {
		t.Errorf("expected empty token, got %s", token)
	}
	c.Set("token", "test-token-123")
	if token := GetTokenFromContext(c); token != "test-token-123" {
		t.Errorf("expected test-token-123, got %s", token)
	}
}
