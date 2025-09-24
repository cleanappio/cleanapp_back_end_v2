package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"voice-assistant-service/config"
	"voice-assistant-service/models"
)

type Client struct {
    apiKey string
    model  string
    client *http.Client
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type ChatRequest struct {
    Model    string        `json:"model"`
    Messages []ChatMessage `json:"messages"`
    Stream   bool          `json:"stream"`
}

type StreamResponse struct {
    Choices []struct {
        Delta struct {
            Content string `json:"content"`
        } `json:"delta"`
        FinishReason string `json:"finish_reason"`
    } `json:"choices"`
}

func NewClient(cfg *config.Config) *Client {
    return &Client{
        apiKey: cfg.OpenAIAPIKey,
        model:  cfg.OpenAIModel,
        client: &http.Client{},
    }
}

func (c *Client) StreamChatCompletion(prompt string) (<-chan models.StreamChunk, error) {
    ch := make(chan models.StreamChunk, 10)
    
    go func() {
        defer close(ch)
        
        messages := []ChatMessage{
            {
                Role:    "system",
                Content: "You are a helpful voice assistant inside a mobile app. Guide users on how to use the app's features. Speak in a friendly, clear tone. Keep responses concise and helpful.",
            },
            {
                Role:    "user",
                Content: prompt,
            },
        }
        
        reqBody := ChatRequest{
            Model:    c.model,
            Messages: messages,
            Stream:   true,
        }
        
        jsonData, err := json.Marshal(reqBody)
        if err != nil {
            ch <- models.StreamChunk{Error: "Failed to marshal request"}
            return
        }
        
        req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
        if err != nil {
            ch <- models.StreamChunk{Error: "Failed to create request"}
            return
        }
        
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+c.apiKey)
        
        resp, err := c.client.Do(req)
        if err != nil {
            ch <- models.StreamChunk{Error: "Failed to make request"}
            return
        }
        defer resp.Body.Close()
        
        if resp.StatusCode != http.StatusOK {
            ch <- models.StreamChunk{Error: fmt.Sprintf("OpenAI API error: %d", resp.StatusCode)}
            return
        }
        
        scanner := bufio.NewScanner(resp.Body)
        for scanner.Scan() {
            line := scanner.Text()
            if strings.HasPrefix(line, "data: ") {
                data := strings.TrimPrefix(line, "data: ")
                if data == "[DONE]" {
                    ch <- models.StreamChunk{Done: true}
                    return
                }
                
                var streamResp StreamResponse
                if err := json.Unmarshal([]byte(data), &streamResp); err == nil {
                    if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
                        ch <- models.StreamChunk{Content: streamResp.Choices[0].Delta.Content}
                    }
                }
            }
        }
    }()
    
    return ch, nil
}