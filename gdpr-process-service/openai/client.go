package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type ObfuscationResult struct {
	Obfuscated string `json:"obfuscated"`
}

// Client represents an OpenAI API client
type Client struct {
	apiKey string
	model  string
	client *http.Client
}

// NewClient creates a new OpenAI client
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// DetectAndObfuscatePII analyzes text for PII and returns obfuscated version
func (c *Client) DetectAndObfuscatePII(text string) (string, error) {
	prompt := fmt.Sprintf(`
Please detect if the text below contains any PII and obfuscate it by replacing of some chars with '*'.

%s 

Consider following data as PII: 
- full name; 
- email address; 
- physical address; 
- credit card data; 
- phone number;
- any other data that can be considered PII.

Please output the result as JSON: 
{ "obfuscated": "value" } 
If no PII detected, put an original value into "obfuscated".`, text)

	reqBody := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	// Parse the JSON response to extract obfuscated value
	content := chatResp.Choices[0].Message.Content

	var result ObfuscationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If JSON parsing fails, return the raw content
		return content, nil
	}

	return result.Obfuscated, nil
}
