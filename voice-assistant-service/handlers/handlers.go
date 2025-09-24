package handlers

import (
	"net/http"
	"voice-assistant-service/models"
	"voice-assistant-service/openai"
	"voice-assistant-service/utils"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

type VoiceHandler struct {
    openaiClient *openai.Client
}

func NewVoiceHandler(openaiClient *openai.Client) *VoiceHandler {
    return &VoiceHandler{
        openaiClient: openaiClient,
    }
}

// HealthCheck returns service health status
func (h *VoiceHandler) HealthCheck(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{
        "status":  "healthy",
        "service": "voice-assistant-service",
    })
}

// Assistant handles the main voice assistant endpoint
func (h *VoiceHandler) Assistant(c *gin.Context) {
    var req models.AssistantRequest
    
    if err := c.BindJSON(&req); err != nil {
        log.Errorf("Failed to bind request: %v", err)
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
        return
    }
    
    if req.Prompt == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Prompt is required"})
        return
    }
    
    // Set headers for streaming
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")
    c.Header("Access-Control-Allow-Origin", "*")
    
    // Get streaming response from OpenAI
    stream, err := h.openaiClient.StreamChatCompletion(req.Prompt)
    if err != nil {
        log.Errorf("Failed to get OpenAI stream: %v", err)
        utils.WriteStreamChunk(c.Writer, models.StreamChunk{
            Error: "Failed to connect to AI service",
        })
        return
    }
    
    // Stream response back to client
    for chunk := range stream {
        if err := utils.WriteStreamChunk(c.Writer, chunk); err != nil {
            log.Errorf("Failed to write stream chunk: %v", err)
            break
        }
        
        if chunk.Done {
            break
        }
    }
}