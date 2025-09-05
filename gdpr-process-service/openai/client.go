package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
)

const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

type Message struct {
	Role    string        `json:"role"`
	Content []ContentItem `json:"content"`
}

type ContentItem struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	ImageURL ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
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

type DocumentDetectionResult struct {
	IsDocument bool `json:"is_document"`
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
				Role: "user",
				Content: []ContentItem{
					{
						Type: "text",
						Text: prompt,
					},
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

	// Parse the JSON response to extract obfuscated value
	content, err := extractJSONFromMarkdown(chatResp.Choices[0].Message.Content)
	if err != nil {
		return "", fmt.Errorf("failed to extract JSON from markdown: %w", err)
	}

	var result ObfuscationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If JSON parsing fails, return the raw content
		log.Printf("ERROR: Failed to parse obfuscation response %s: %v", content, err)
		return text, nil
	}

	// Check if result.Obfuscated contains '\n' and truncate at first newline
	obfuscated := result.Obfuscated
	if newlineIndex := strings.Index(obfuscated, "\n"); newlineIndex != -1 {
		obfuscated = obfuscated[:newlineIndex]
	}

	return obfuscated, nil
}

// DetectDocument analyzes an image to determine if it contains a document with potential PII
func (c *Client) DetectDocument(imageData []byte) (bool, error) {
	// Encode image data to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Create data URL for the image
	dataURL := fmt.Sprintf("data:image/jpeg;base64,%s", base64Image)

	prompt := `Please analyze the image and answer if the image contains a photo or a scan of a document that might contain any PII.

Output the answer as JSON:
{
  "is_document": <true|false>
}`

	reqBody := ChatRequest{
		Model: c.model,
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentItem{
					{
						Type: "text",
						Text: prompt,
					},
					{
						Type: "image_url",
						ImageURL: ImageURL{
							URL: dataURL,
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return false, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return false, fmt.Errorf("no choices in response")
	}

	// Parse the JSON response to extract is_document value
	content, err := extractJSONFromMarkdown(chatResp.Choices[0].Message.Content)
	if err != nil {
		return false, fmt.Errorf("failed to extract JSON from markdown: %w", err)
	}

	var result DocumentDetectionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If JSON parsing fails, assume it's not a document for safety
		return false, fmt.Errorf("failed to parse document detection response: %w", err)
	}

	return result.IsDocument, nil
}

// extractJSONFromMarkdown extracts JSON from markdown code blocks
func extractJSONFromMarkdown(content string) (string, error) {
	// Try to find JSON in ```json or ```JSON code blocks
	jsonRegex := regexp.MustCompile("```(?:json|JSON)?\\s*\\n?([\\s\\S]*?)\\n?```")
	matches := jsonRegex.FindStringSubmatch(content)

	if len(matches) > 1 {
		// Found JSON in code block, clean it up
		jsonStr := strings.TrimSpace(matches[1])
		return jsonStr, nil
	}

	// If no code block found, try to find JSON-like content
	// Look for content that starts with { and ends with }
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")

	if start != -1 && end != -1 && end > start {
		jsonStr := content[start : end+1]
		return jsonStr, nil
	}

	// If no JSON found, return the original content
	return content, nil
}
