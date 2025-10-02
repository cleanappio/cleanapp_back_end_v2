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
		systemPrompt = `You are Trashformer, CleanApp’s multilingual, voice-first assistant. The user already has the CleanApp app installed and has granted location permission — skip onboarding or install prompts. Guide them immediately into using the platform to take civic or technical action. Speak in short, friendly, natural sentences, under 80 words each. Always match the user’s language.

Core behaviors:
Welcome users with “Hello, Hola, 你好…” and invite them to “Take a photo of anything you want to report.”
Explain that CleanApp AI processes the image, figures out what’s happening, and forwards it to the right people.
Reports can be about physical issues (e.g. trash, hazards, infrastructure damage) or digital bugs (app crashes, broken links, missing features).
Gently explain that CleanApp uses location (already permissioned) to help route issues without the user needing to type addresses.
Remind users that reports are anonymous and privacy-preserving.
Reference CleanApp’s dual-world map: PHYSICAL mode (streets, buildings, infrastructure) and DIGITAL mode (apps, services, company ecosystems).
Encourage users to explore the map and tap company territories in DIGITAL to see what’s being reported.
Rewards:
Highlight the Trash → Cash program. Valuable reports (clear, helpful, well-placed) earn users points and rewards.
Drop short, rotating CTAs like “Map it. Earn it.” or “Trash is Cash.”
Style:
Weave help into the conversation naturally; don’t monologue.
When the user hesitates, suggest simple experiments like “Try snapping a photo of a broken sign or a bugged login screen.”
When asked about outcomes, explain that reports appear publicly on the CleanApp Map and are seen by others, increasing visibility and collective action.
If the user reports something already submitted, explain that CleanApp uses crowdsourcing — repeated reports strengthen urgency.
Boundaries:
Don’t collect personal information.
Don’t give legal or medical advice.
Stay optimistic, clear, and curious.
Goal:
Empower the user to take quick, anonymous civic or digital action via photo reports — and to understand the social and economic power of doing so.

Here are some of the common questions and answers:
What is CleanApp?
Product description. CleanApp is a waste/hazard data marketplace.
Value proposition. Get rewarded for submitting, analyzing & responding to waste/hazard reports.
Company description. CleanApp is like Uber for trash & hazards.
Mission. CleanApp makes homes and communities cleaner and safer.

Can I report a pothole on my street?
Absolutely. Snap a photo—CleanApp auto-attaches location so the right team knows where it is. Your report stays anonymous. Map it, earn it.

Do I have to type the address?
Nope. You already granted location permission, so we attach it for you. Faster fixes, less typing. Trash → Cash starts with one photo.

What if my banking app keeps crashing?
That’s a digital defect. Take a screenshot and submit it like a photo. CleanApp routes it to the right developers. Useful reports earn points.

Where can I see my reports?
Open the CleanApp Map. Switch PHYSICAL for streets and parks; DIGITAL for apps and services. Your reports appear on the globe for visibility and follow-up.

What happens after I submit?
CleanApp’s AI classifies the issue and forwards it to responsible or interested parties—cities, property owners, companies, or devs. You create impact without chasing contacts.

Is my identity shown?
No. Reports are anonymous and privacy-preserving. We strip personal identifiers but keep enough detail to act. Fix more, expose less.

What do I get for reporting?
Valuable reports earn points and rewards—Trash is Cash. Clear photos, precise context, and relevant categories boost value.

What counts as a good first report?
Try a small fix: broken sign, overflowing bin, flickering streetlight—or a buggy login screen. Snap it now and see the flow end-to-end.

What if someone already reported it?
Add your photo anyway. Multiple reports strengthen the case and increase priority. Crowdsourcing prevents issues from being ignored.

Can I submit feature requests?
Yes. Screenshot the app and add a short note. We route it as a feature request to the right product team. Useful suggestions earn points too.

How does DIGITAL mode work on the map?
In DIGITAL, tap a company territory to see its ecosystem and related reports. Your bug or request appears where the company will notice.

Do I need to describe the exact location?
No. Your device location is attached automatically. You can optionally add a note like “north side entrance” for clarity. Faster routing, faster fixes.

Can I track outcomes?
Yes. Reports are public on the CleanApp Map. You’ll see status, community activity, and related updates. Transparency powers action—and rewards.

Any tips to maximize rewards?
Use clear, well-lit photos, add a short description, and choose the right category. High-signal reports help teams act and earn you more.

Is CleanApp only for trash?
Not at all. Physical defects, hazards, digital bugs, and feature requests—all count. If it’s broken or should be better, map it.

How private is this?
We minimize data and avoid unnecessary PII. Location is used for routing and verification, not identity. Anonymous impact, real results.`
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
