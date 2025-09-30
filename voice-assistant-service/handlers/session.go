package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"voice-assistant-service/config"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type SessionHandler struct {
	config *config.Config
}

func NewSessionHandler(cfg *config.Config) *SessionHandler {
	return &SessionHandler{
		config: cfg,
	}
}

type CreateSessionRequest struct {
	Model       string                 `json:"model"`
	Voice       string                 `json:"voice,omitempty"`
	SystemPrompt string                `json:"system_prompt,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type CreateSessionResponse struct {
	SessionID     string                 `json:"session_id"`
	ClientSecret  map[string]interface{} `json:"client_secret"`
	ExpiresAt     string                 `json:"expires_at,omitempty"`
	IceServers    []map[string]interface{} `json:"ice_servers,omitempty"`
	SystemPrompt  string                 `json:"system_prompt,omitempty"`
}

type OpenAISessionResponse struct {
	ID           string                 `json:"id"`
	ClientSecret map[string]interface{} `json:"client_secret"`
	ExpiresAt    interface{}            `json:"expires_at,omitempty"`
	IceServers   []map[string]interface{} `json:"ice_servers,omitempty"`
}

// CreateEphemeralSession creates an ephemeral OpenAI Realtime session
func (h *SessionHandler) CreateEphemeralSession(c *gin.Context) {
	// Use client IP as identifier for rate limiting and logging
	clientIP := c.ClientIP()
	userID := "client_" + clientIP

	apiKey := h.config.OpenAIAPIKey
	if apiKey == "" {
		log.Error("TRASHFORMER_OPENAI_API_KEY not configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server misconfigured"})
		return
	}

	// Parse request body
	var reqBody CreateSessionRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		log.Warnf("Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Set default model if not provided
	if reqBody.Model == "" {
		reqBody.Model = h.config.OpenAIModel
	}

	// Log session creation request (without sensitive data)
	log.WithFields(log.Fields{
		"user_id": userID,
		"model":   reqBody.Model,
		"voice":   reqBody.Voice,
	}).Info("session.create.request")

	// Set system prompt (use provided one or default)
	systemPrompt := reqBody.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = `You are Trashformer, CleanApp's voice + chat guide. Begin every new conversation with:

			"Hello, Hola, 你好… welcome to CleanApp! Talk to me in any language to learn how to turn Trash into Cash."

			PRINCIPLES:
			- Always reply in the user's language.
			- Keep replies under 50 words, friendly, clear, action-oriented.
			- End most answers with a 1-line CTA about Trash→Cash (earning, rewards, payouts, partners, leaderboards).

			WHAT TO TEACH (succinctly):
			1) Dual-world globe: PHYSICAL Earth vs DIGITAL cyberspace.
			2) Report types: waste, hazards, bugs, and general feedback.
			3) CleanApp forwards reports to companies, authorities, and developers.
			4) Switch between PHYSICAL/DIGITAL; in DIGITAL, tap a company's territory to see its ecosystem.
			5) Access CLEANAPPMAP and CLEANAPPGPT from the menu for exploring and guidance.

			TRASH → CASH FOCUS:
			- Proactively explain economic incentives: how reports gain value, how to maximize impact (clear photos, precise location, short description), and where rewards show up.
			- When users ask "how do I start" or "how do I earn," give a concrete next step (e.g., make a first report, add a photo, check local opportunities, view leaderboard).

			APP INSTALL CTA:
			- If users ask how to submit reports: direct them to download CleanApp on the Apple App Store or Google Play, joining 500,000+ people worldwide.

			TONE & SAFETY:
			- Be encouraging, never judgmental. Do not request or store unnecessary PII. No legal/medical advice.

			EXAMPLES OF ENDING CTAs (rotate naturally):
			- "Ready to turn your first report into rewards?"
			- "Snap a photo—earn while you help your community."
			- "Map it now; cash in later."

			Your job: teach features, guide first actions, and keep spotlighting how to turn Trash into Cash.`
	}

	// Build OpenAI request payload
	payload := map[string]interface{}{
		"model": reqBody.Model,
	}
	if reqBody.Voice != "" {
		payload["voice"] = reqBody.Voice
	}
	if reqBody.Metadata != nil {
		payload["metadata"] = reqBody.Metadata
	}
	if systemPrompt != "" {
		payload["instructions"] = systemPrompt
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("Failed to marshal OpenAI request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Create HTTP request to OpenAI
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/realtime/sessions", bytes.NewReader(payloadBytes))
	if err != nil {
		log.Errorf("Failed to create OpenAI request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Make request to OpenAI
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("OpenAI request failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to contact OpenAI"})
		return
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read OpenAI response: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to read OpenAI response"})
		return
	}

	// Handle OpenAI errors
	if resp.StatusCode >= 400 {
		log.Errorf("OpenAI session create returned %d: %s", resp.StatusCode, string(respBytes))
		
		switch resp.StatusCode {
		case 401:
			c.JSON(http.StatusBadGateway, gin.H{"error": "OpenAI authentication failed"})
			return
		case 429:
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limited by OpenAI"})
			return
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "OpenAI session create failed"})
			return
		}
	}

	// Parse OpenAI response
	var openaiResp OpenAISessionResponse
	if err := json.Unmarshal(respBytes, &openaiResp); err != nil {
		log.Errorf("Failed to parse OpenAI response: %v", err)
		log.Errorf("OpenAI response body: %s", string(respBytes))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Invalid response from OpenAI"})
		return
	}

	// Build response
	var expiresAtStr string
	
	// Try to get expires_at from client_secret first (this is where it actually is)
	if openaiResp.ClientSecret != nil {
		if clientSecretExpiresAt, ok := openaiResp.ClientSecret["expires_at"]; ok {
			switch v := clientSecretExpiresAt.(type) {
			case float64:
				expiresAtStr = fmt.Sprintf("%.0f", v)
			case int64:
				expiresAtStr = fmt.Sprintf("%d", v)
			case int:
				expiresAtStr = fmt.Sprintf("%d", v)
			case string:
				expiresAtStr = v
			default:
				expiresAtStr = fmt.Sprintf("%v", v)
			}
		}
	}
	
	// If not found in client_secret, try the top-level field
	if expiresAtStr == "" && openaiResp.ExpiresAt != nil {
		switch v := openaiResp.ExpiresAt.(type) {
		case float64:
			expiresAtStr = fmt.Sprintf("%.0f", v)
		case int64:
			expiresAtStr = fmt.Sprintf("%d", v)
		case int:
			expiresAtStr = fmt.Sprintf("%d", v)
		case string:
			expiresAtStr = v
		default:
			expiresAtStr = fmt.Sprintf("%v", v)
		}
	}
	
	// If still empty, set to "0" as fallback
	if expiresAtStr == "" {
		expiresAtStr = "0"
	}

	response := CreateSessionResponse{
		SessionID:    openaiResp.ID,
		ClientSecret: openaiResp.ClientSecret,
		ExpiresAt:    expiresAtStr,
		IceServers:   openaiResp.IceServers,
		SystemPrompt: systemPrompt,
	}

	// Add custom ICE servers if configured
	if turnServers := h.config.GetTurnServers(); len(turnServers) > 0 {
		customIceServers := make([]map[string]interface{}, len(turnServers))
		for i, server := range turnServers {
			customIceServers[i] = map[string]interface{}{
				"urls":       server.URLs,
				"username":   server.Username,
				"credential": server.Credential,
			}
		}
		response.IceServers = append(response.IceServers, customIceServers...)
	}

	// Log successful session creation (without sensitive data)
	log.WithFields(log.Fields{
		"user_id":    userID,
		"session_id": openaiResp.ID,
		"expires_at": expiresAtStr,
	}).Info("session.create.success")

	c.JSON(http.StatusOK, response)
}

// PrewarmSession creates a session and caches it for a short period
func (h *SessionHandler) PrewarmSession(c *gin.Context) {
	// For now, just call the regular session creation
	// In a production system, you might want to implement caching
	h.CreateEphemeralSession(c)
}
