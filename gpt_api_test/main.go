package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// getMimeType returns the MIME type based on file extension
func getMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg" // default fallback
	}
}

// encodeImageToBase64 reads an image file and returns it as a base64 data URL
func encodeImageToBase64(imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to open image file: %w", err)
	}
	defer file.Close()

	imageData, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read image file: %w", err)
	}

	mimeType := getMimeType(imagePath)
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <image_path>")
		fmt.Println("Example: go run main.go ./image.jpg")
		return
	}

	imagePath := os.Args[1]

	// Encode the image to base64
	base64Image, err := encodeImageToBase64(imagePath)
	if err != nil {
		fmt.Printf("Error encoding image: %v\n", err)
		return
	}

	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Println("OPENAI_API_KEY is not set")
		return
	}

	textPrompt := TextContent{
		Type: "text",
		Text: "What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo in units from 0.0 to 1.0 and a severity level from 0.0 to 1.0",
	}

	imagePrompt := ImageContent{
		Type: "image_url",
		ImageURL: ImageURL{
			URL: base64Image,
		},
	}

	reqBody := ChatRequest{
		Model: "gpt-4o",
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
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+openAIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Println("API error:", string(body))
		return
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		fmt.Println("Error parsing response:", err)
		return
	}

	// Display the text response
	fmt.Println("Response from model:")
	for _, choice := range chatResp.Choices {
		fmt.Printf("%+v\n", choice.Message.Content)
	}
}
