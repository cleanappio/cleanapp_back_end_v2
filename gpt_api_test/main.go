package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

/*
This program demonstrates image analysis using both OpenAI GPT-4 Vision API and Google Gemini API.

Features:
- Image analysis with OpenAI GPT-4 Vision API
- Image analysis with Google Gemini API
- Text translation with both APIs
- Automatic URL detection to choose appropriate analysis prompts
- Support for multiple image formats (JPEG, PNG, GIF, WebP)

Environment Variables:
- OPENAI_API_KEY: Required for OpenAI API calls
- GEMINI_API_KEY: Optional for Gemini API calls

Usage:
	go run main.go <image_path>
	Example: go run main.go ./image.jpg

The program will:
1. Classify the image to determine if it contains a website/URL
2. Analyze the image with appropriate prompt based on classification
3. Translate the analysis to Montenegrin
4. If GEMINI_API_KEY is set, also demonstrate Gemini API usage
*/

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

const litterHazardPromptForGemini = `
What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, please extract any brand name from the image, if present. Also, give a probability that there is a litter or hazard on a photo in units from 0.0 to 1.0 and a severity level from 0.0 to 1.0. The severity means a degree of how dangerous for life the object on the image is.
If there is no visible litter or hazard on the image, please specifify that explicitly.
Analyze this image and provide a JSON response with the following structure:
{
"title": "A descriptive title for the issue",
"description": "A detailed description of what you see in the image",
"brand_name": "A brand name, if present, otherwise null",
"litter_probability": 0.0-1.0,
"hazard_probability": 0.0-1.0,
"severity_level": 0.0-1.0
}
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

// extractBase64FromDataURL extracts the base64 data from a data URL
func extractBase64FromDataURL(dataURL string) (string, error) {
	// Check if it's a data URL
	if !strings.HasPrefix(dataURL, "data:") {
		return dataURL, nil // Already just base64 data
	}

	// Find the comma that separates the header from the data
	commaIndex := strings.Index(dataURL, ",")
	if commaIndex == -1 {
		return "", fmt.Errorf("invalid data URL format")
	}

	// Extract the base64 data part
	return dataURL[commaIndex+1:], nil
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

	// Check for Gemini API key
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		fmt.Println("GEMINI_API_KEY is not set (optional)")
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

	// Step 5: Example of using Gemini API (if API key is available)
	if geminiKey != "" {
		fmt.Println("\n=== Gemini API Example ===")

		// Use Gemini for image analysis
		fmt.Println("Step 5: Analyzing with Gemini...")
		geminiResponse, err := CallGemini(geminiKey, base64Image, litterHazardPromptForGemini)
		if err != nil {
			fmt.Printf("Error in Gemini analysis call: %v\n", err)
		} else {
			fmt.Printf("Gemini analysis response: %s\n", geminiResponse)
		}

		// Use Gemini for translation
		fmt.Println("Step 6: Translating with Gemini...")
		geminiTranslation, err := CallGeminiTranslation(geminiKey, analysisResponse, "Montenegrin")
		if err != nil {
			fmt.Printf("Error in Gemini translation call: %v\n", err)
		} else {
			fmt.Printf("Gemini translation response: %s\n", geminiTranslation)
		}
	} else {
		fmt.Println("\nSkipping Gemini API example (GEMINI_API_KEY not set)")
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

// CallGemini makes a call to Gemini API with the given prompt and image
// Parameters:
//   - apiKey: Google AI API key for authentication
//   - base64Image: Base64 encoded image data URL
//   - prompt: The text prompt to send with the image
//
// Returns:
//   - The response text as a string
//   - An error if the API call fails
//
// Example usage:
//
//	response, err := CallGemini(apiKey, base64Image, "Analyze this image")
//	if err != nil {
//	    log.Printf("Gemini API error: %v", err)
//	} else {
//	    fmt.Printf("Response: %s", response)
//	}
func CallGemini(apiKey, base64Image, prompt string) (string, error) {
	// Extract base64 data from data URL if needed
	imageData, err := extractBase64FromDataURL(base64Image)
	if err != nil {
		return "", fmt.Errorf("error extracting base64 data: %w", err)
	}

	// Create context
	ctx := context.Background()

	// Create client with API key
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("error creating Gemini client: %w", err)
	}
	defer client.Close()

	// Get the model
	model := client.GenerativeModel("gemini-1.5-flash")

	// Decode base64 image data
	imageBytes, err := base64.StdEncoding.DecodeString(imageData)
	if err != nil {
		return "", fmt.Errorf("error decoding base64 image: %w", err)
	}

	// Create image part
	imagePart := genai.ImageData("image/jpeg", imageBytes)

	// Create content with text and image
	content := []genai.Part{
		genai.Text(prompt),
		imagePart,
	}

	// Generate content
	resp, err := model.GenerateContent(ctx, content...)
	if err != nil {
		return "", fmt.Errorf("error generating content: %w", err)
	}

	// Check if we have candidates
	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}

	// Extract text from the first candidate
	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return "", fmt.Errorf("no parts in candidate content")
	}

	// Return the text from the first part
	part := candidate.Content.Parts[0]
	if textPart, ok := part.(genai.Text); ok {
		return string(textPart), nil
	}

	return "", fmt.Errorf("unexpected response format")
}

// CallGeminiTranslation makes a call to Gemini API for text translation
// Parameters:
//   - apiKey: Google AI API key for authentication
//   - text: The text to be translated
//   - targetLanguage: The target language to translate into (e.g., "Spanish", "French", "Montenegrin")
//
// Returns:
//   - The translated text as a string
//   - An error if the translation fails
//
// Example usage:
//
//	translatedText, err := CallGeminiTranslation(apiKey, "Hello world", "Spanish")
//	if err != nil {
//	    log.Printf("Translation error: %v", err)
//	} else {
//	    fmt.Printf("Translated: %s", translatedText)
//	}
func CallGeminiTranslation(apiKey, text, targetLanguage string) (string, error) {
	translationPrompt := fmt.Sprintf("Please translate the following text to %s:\n\n%s", targetLanguage, text)

	// Create context
	ctx := context.Background()

	// Create client with API key
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("error creating Gemini client: %w", err)
	}
	defer client.Close()

	// Get the model
	model := client.GenerativeModel("gemini-1.5-flash")

	// Create content with text only
	content := []genai.Part{
		genai.Text(translationPrompt),
	}

	// Generate content
	resp, err := model.GenerateContent(ctx, content...)
	if err != nil {
		return "", fmt.Errorf("error generating content: %w", err)
	}

	// Check if we have candidates
	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}

	// Extract text from the first candidate
	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return "", fmt.Errorf("no parts in candidate content")
	}

	// Return the text from the first part
	part := candidate.Content.Parts[0]
	if textPart, ok := part.(genai.Text); ok {
		return string(textPart), nil
	}

	return "", fmt.Errorf("unexpected response format")
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
