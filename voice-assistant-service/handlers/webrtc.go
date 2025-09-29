package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type WebRTCHandler struct{}

func NewWebRTCHandler() *WebRTCHandler {
	return &WebRTCHandler{}
}

type ProxyOfferRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	EphemeralKey string `json:"ephemeral_key" binding:"required"`
	OfferSDP     string `json:"offer_sdp" binding:"required"`
}

// ProxyOffer proxies a WebRTC offer to OpenAI and returns the answer SDP
func (h *WebRTCHandler) ProxyOffer(c *gin.Context) {
	// Use client IP as identifier for logging
	clientIP := c.ClientIP()
	userID := "client_" + clientIP

	// Parse request body
	var req ProxyOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf("Invalid proxy offer request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	log.WithFields(log.Fields{
		"user_id":    userID,
		"session_id": req.SessionID,
	}).Info("webrtc.proxy_offer.request")

	// Create request to OpenAI WebRTC endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reqBody := bytes.NewReader([]byte(req.OfferSDP))
	openaiURL := "https://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview"
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", openaiURL, reqBody)
	if err != nil {
		log.Errorf("Failed to create OpenAI WebRTC request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	httpReq.Header.Set("Authorization", "Bearer "+req.EphemeralKey)
	httpReq.Header.Set("Content-Type", "application/sdp")

	// Make request to OpenAI
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Errorf("OpenAI WebRTC request failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to contact OpenAI"})
		return
	}
	defer resp.Body.Close()

	// Handle OpenAI errors
	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		log.Errorf("OpenAI WebRTC returned %d: %s", resp.StatusCode, string(respBytes))
		
		switch resp.StatusCode {
		case 401:
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid ephemeral key"})
			return
		case 403:
			c.JSON(http.StatusForbidden, gin.H{"error": "Session expired or invalid"})
			return
		case 429:
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limited by OpenAI"})
			return
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "OpenAI WebRTC request failed"})
			return
		}
	}

	// Read the answer SDP
	answerSDP, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read OpenAI WebRTC response: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to read OpenAI response"})
		return
	}

	log.WithFields(log.Fields{
		"user_id":    userID,
		"session_id": req.SessionID,
	}).Info("webrtc.proxy_offer.success")

	// Return the answer SDP
	c.Header("Content-Type", "application/sdp")
	c.String(http.StatusOK, string(answerSDP))
}
