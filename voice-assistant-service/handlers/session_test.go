package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"voice-assistant-service/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCreateEphemeralSession_ValidRequest(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		OpenAIAPIKey: "test-key",
		OpenAIModel:  "gpt-4o-realtime-preview",
	}
	handler := NewSessionHandler(cfg)

	// Create test request
	reqBody := CreateSessionRequest{
		Model: "gpt-4o-realtime-preview",
		Voice: "alloy",
		Metadata: map[string]interface{}{
			"client_app": "test-app",
		},
	}
	jsonBody, _ := json.Marshal(reqBody)

	// Create request
	req := httptest.NewRequest("POST", "/session", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	// Create response recorder
	w := httptest.NewRecorder()

	// Create gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Mock user context (normally set by auth middleware)
	c.Set("user_id", "test-user")

	// Call handler
	handler.CreateEphemeralSession(c)

	// Assertions
	assert.Equal(t, http.StatusBadGateway, w.Code) // Will fail due to missing OpenAI API call
}

func TestCreateEphemeralSession_InvalidRequest(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		OpenAIAPIKey: "test-key",
		OpenAIModel:  "gpt-4o-realtime-preview",
	}
	handler := NewSessionHandler(cfg)

	// Create invalid request (empty body)
	req := httptest.NewRequest("POST", "/session", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	// Create response recorder
	w := httptest.NewRecorder()

	// Create gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Mock user context
	c.Set("user_id", "test-user")

	// Call handler
	handler.CreateEphemeralSession(c)

	// Assertions
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateEphemeralSession_MissingAPIKey(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		OpenAIAPIKey: "", // Empty API key
		OpenAIModel:  "gpt-4o-realtime-preview",
	}
	handler := NewSessionHandler(cfg)

	// Create test request
	reqBody := CreateSessionRequest{
		Model: "gpt-4o-realtime-preview",
	}
	jsonBody, _ := json.Marshal(reqBody)

	// Create request
	req := httptest.NewRequest("POST", "/session", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	// Create response recorder
	w := httptest.NewRecorder()

	// Create gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Mock user context
	c.Set("user_id", "test-user")

	// Call handler
	handler.CreateEphemeralSession(c)

	// Assertions
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
