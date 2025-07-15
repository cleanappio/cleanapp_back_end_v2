package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type ImageContent struct {
	Type     string   `json:"type"`
	ImageURL ImageURL `json:"image_url"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
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

// encodeImageToBase64 converts image bytes to base64 data URL
func encodeImageToBase64(imageData []byte) string {
	base64Data := base64.StdEncoding.EncodeToString(imageData)
	return fmt.Sprintf("data:image/jpeg;base64,%s", base64Data)
}

// AnalyzeImage analyzes an image using OpenAI's vision API
func (c *Client) AnalyzeImage(imageData []byte, prompt string) (string, error) {
	textPrompt := TextContent{
		Type: "text",
		Text: prompt,
	}

	imagePrompt := ImageContent{
		Type: "image_url",
		ImageURL: ImageURL{
			URL: encodeImageToBase64(imageData),
		},
	}

	reqBody := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{
				Role: "user",
				Content: []any{
					textPrompt,
					imagePrompt,
				},
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

	// Extract the text content from the response
	content := chatResp.Choices[0].Message.Content
	if contentStr, ok := content.(string); ok {
		return contentStr, nil
	}

	// If content is not a string, try to marshal it back to JSON
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}

	return string(contentJSON), nil
}

func (c *Client) TranslateAnalysis(text, targetLanguage string) (string, error) {
	translationPrompt := fmt.Sprintf("Please translate the following text to %s:\n\n%s", targetLanguage, text)

	reqBody := ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{
				Role:    "user",
				Content: translationPrompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	// Extract the text content from the response
	content := chatResp.Choices[0].Message.Content
	if contentStr, ok := content.(string); ok {
		return contentStr, nil
	}

	// If content is not a string, try to marshal it back to JSON
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("error marshaling content: %w", err)
	}

	return string(contentJSON), nil
}
