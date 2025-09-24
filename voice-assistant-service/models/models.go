package models

// AssistantRequest represents the incoming request
type AssistantRequest struct {
    Prompt string `json:"prompt" binding:"required"`
}

// StreamChunk represents a chunk of streaming response
type StreamChunk struct {
    Content string `json:"content"`
    Done    bool   `json:"done"`
    Error   string `json:"error,omitempty"`
}