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

const litterHazardPrompt = `
	What kind of litter or hazard can you see on this image?
				
	Please describe the litter or hazard in detail.
	Also, give a probability that there is a litter or hazard on a photo in units from 0.0 to 1.0 and
	a severity level from 0.0 to 1.0.
	Also, please extract any brand name from the image, if present.

	Please format the output as a json with following fields:
	- title: an issue title, one sentence;
	- description: a description, one paragraph;
	- brand_name: optional, a brand name, if present;
	- litter_probability: a probability, a number from 0.0 to 1.0;
	- hazard_probability: a severity, a number from 0.0 to 1.0;
	- severity_level: a severity level, a number from 0.0 to 1.0;
`

const digitalErrorPrompt = `
	Do you see a bug on the web page on the image?
	Please provide a bug, please output its description if you recognize it.
	
	Please format the output as:
	- a bug title, one sentence;
	- a bug description, one paragraph;
	- a brand name, optional, if present;
	- a site URL, if you recognize it;
	- a probability, a number from 0.0 to 1.0;
	- a severity, a number from 0.0 to 1.0;
`

const classificationPrompt = `
	Do you see a site on a computer screen on this image?
	Please provide a site on the screen, please output its URL if you recognize it.

	Please format the output as:
	- a site on the screen, please output its URL if you recognize it;
`

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

	// Step 1: Call API with classificationPrompt
	fmt.Println("Step 1: Classifying image...")
	classificationResponse, err := callOpenAI(openAIKey, base64Image, classificationPrompt)
	if err != nil {
		fmt.Printf("Error in classification call: %v\n", err)
		return
	}

	fmt.Printf("Classification response: %s\n\n", classificationResponse)

	// Step 2: Check if URL is found in the response
	hasURL := containsURL(classificationResponse)

	var secondPrompt string
	if hasURL {
		fmt.Println("URL detected. Using digital error prompt...")
		secondPrompt = digitalErrorPrompt
	} else {
		fmt.Println("No URL detected. Using litter/hazard prompt...")
		secondPrompt = litterHazardPrompt
	}

	// Step 3: Call API with the appropriate prompt
	fmt.Println("Step 2: Analyzing with appropriate prompt...")
	analysisResponse, err := callOpenAI(openAIKey, base64Image, secondPrompt)
	if err != nil {
		fmt.Printf("Error in analysis call: %v\n", err)
		return
	}

	fmt.Printf("Analysis response: %s\n", analysisResponse)

	// Step 4: Translate the response to Montenegro language
	fmt.Println("Step 4: Translating to Montenegro language...")
	translationResponse, err := callOpenAITranslation(openAIKey, analysisResponse, "Montenegrin")
	if err != nil {
		fmt.Printf("Error in translation call: %v\n", err)
		return
	}

	fmt.Printf("Translation response: %s\n", translationResponse)

	// Example of using the translation function for other languages
	fmt.Println("\nExample: Translating to Spanish...")
	spanishTranslation, err := callOpenAITranslation(openAIKey, "Hello, how are you?", "Spanish")
	if err != nil {
		fmt.Printf("Error in Spanish translation: %v\n", err)
	} else {
		fmt.Printf("Spanish translation: %s\n", spanishTranslation)
	}
}

// callOpenAI makes a call to OpenAI API with the given prompt and image
func callOpenAI(apiKey, base64Image, prompt string) (string, error) {
	textPrompt := TextContent{
		Type: "text",
		Text: prompt,
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
		return "", fmt.Errorf("error marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", openAIEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
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

// callOpenAITranslation makes a call to OpenAI API for text translation
// Parameters:
//   - apiKey: OpenAI API key for authentication
//   - text: The text to be translated
//   - targetLanguage: The target language to translate into (e.g., "Spanish", "French", "Montenegrin")
//
// Returns:
//   - The translated text as a string
//   - An error if the translation fails
//
// Example usage:
//
//	translatedText, err := callOpenAITranslation(apiKey, "Hello world", "Spanish")
//	if err != nil {
//	    log.Printf("Translation error: %v", err)
//	} else {
//	    fmt.Printf("Translated: %s", translatedText)
//	}
func callOpenAITranslation(apiKey, text, targetLanguage string) (string, error) {
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
	req.Header.Set("Authorization", "Bearer "+apiKey)
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

// containsURL checks if the response contains a URL
func containsURL(response string) bool {
	// Simple URL detection - look for common URL patterns
	urlPatterns := []string{
		"http://",
		"https://",
		"www.",
		".com",
		".org",
		".net",
		".edu",
		".gov",
		".io",
		".co",
		".uk",
		".de",
		".fr",
		".jp",
		".cn",
	}

	responseLower := strings.ToLower(response)
	for _, pattern := range urlPatterns {
		if strings.Contains(responseLower, pattern) {
			return true
		}
	}

	return false
}
