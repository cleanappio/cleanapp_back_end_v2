package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

type ImageComparisonResult struct {
	SamePlaceProbability  float64 `json:"same_place_probability"`
	LitterOrHazardRemoved bool    `json:"litter_or_hazard_removed"`
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

// CompareImages compares two images using OpenAI's vision API
func (c *Client) CompareImages(image1, image2 []byte, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng float64) (float64, bool, error) {
	// Encode images to base64
	base64Image1 := base64.StdEncoding.EncodeToString(image1)
	base64Image2 := base64.StdEncoding.EncodeToString(image2)

	// Create data URLs for the images
	dataURL1 := fmt.Sprintf("data:image/jpeg;base64,%s", base64Image1)
	dataURL2 := fmt.Sprintf("data:image/jpeg;base64,%s", base64Image2)

	prompt := fmt.Sprintf(`Please analyze these two images and tell me if this is the same place where the photo was taken.
Also, please analyze if a litter or hazard object on the image 1 was removed on the image 2.
Consider locations of each photo.
First image location lat/lng: %f,%f
Second image location lat/lng: %f,%f


Please output the answer as JSON:
{
  "same_place_probability": [0.0-1.0],
  "litter_or_hazard_removed": true|false
}`, firstImageLocationLat, firstImageLocationLng, secondImageLocationLat, secondImageLocationLng)

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
							URL: dataURL1,
						},
					},
					{
						Type: "image_url",
						ImageURL: ImageURL{
							URL: dataURL2,
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0.0, false, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0.0, false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0.0, false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0.0, false, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0.0, false, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return 0.0, false, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return 0.0, false, fmt.Errorf("no choices in response")
	}

	// Parse the JSON response to extract comparison results
	content := chatResp.Choices[0].Message.Content

	var result ImageComparisonResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If JSON parsing fails, log the error and return default values
		log.Printf("ERROR: Failed to parse image comparison response %s: %v", content, err)
		return 0.0, false, fmt.Errorf("failed to parse comparison response: %w", err)
	}

	return result.SamePlaceProbability, result.LitterOrHazardRemoved, nil
}
