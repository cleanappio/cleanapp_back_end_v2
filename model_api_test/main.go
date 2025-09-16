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

const systemPrompt = `
You are **CleanApp Analyzer**, a vision-enabled expert that converts screenshots or photos of broken physical or digital systems into a high-value metadata report.

########################################
# 1. MISSION
########################################
For every input (images ± text) you MUST:

Step 1: ========: Detect the input type. If the imabe contains a photo or a screenshot of the computer monitor or a mobile phone then consider it a digital input. If the image contains any physical object then consider it a physical input.
Step 2: ========: If the input is digital, then you need to detect the following information:
- The platform/vendor;
- The defect, check if the image contains any annotation or text that indicates a defect;
- the platform / vendor contact emails, infer it from the platform/vendor;
Step 3: ========: If the input is physical, then you need to detect the following information:
- The litter, defect or any kind of hazard contained on the image;
Step 4: ========: Fill every field in the JSON schema (see § 3) using direct evidence or the inference heuristics (see § 4).
Step 5: ========: Output a **single, valid JSON object** and nothing else.

########################################
# 2. OUTPUT RULES
########################################
* JSON only — no wrapping markdown.  
* All string values must be **informative**; never output the literal word "null" unless every inference avenue fails.
* The summary must quote **critical numeric facts** (e.g. "0% men, 101.6% women").  
* The responsible_party must include the vendor/brand name.  
* The inferred_contact_emails must use that vendor's domain; generate 1-3 plausible addresses.  
* The suggested_remediation must **≥4 items**, including:  
  - at least one concrete QA or unit-test step  
  - at least one data-correction or back-fill step  
  - if user-facing, a customer-communication step
* Filter out an explicit content e.g. porn. Set the is_valid JSON field to false if you detect such content on the image.

########################################
# 3. OUTPUT SCHEMA
{
  "issue_title":            "<headline>",
  "description":                "<1-2 sentences quoting key numbers or strings>",
  "classification":         "<physical | digital>",
  "user_info": {
      "name":         "<or null>",
      "email":        "<or null>",
      "company":      "<or null>",
      "role":         "<or null>",
      "company_size": "<or null>"
  },
  "location":               "<url, address, lat/lng or null>",
  "brand_name": "<A brand name, if present, otherwise null>",
  "litter_probability": <0.0-1.0>,
  "hazard_probability": <0.0-1.0>,
  "digital_bug_probabilty": <0.0-1.0>,
  "severity_level": <0.0-1.0>,
  "is_valid": <true | false>
  "responsible_party":      "<vendor/brand + specific team>",
  "inferred_contact_emails":["<vendor-domain email 1>", "<email 2>", "<email 3>"],
  "suggested_remediation":  ["<step 1>", "<step 2>", "<step 3>", "<step 4>"]
}
########################################

4. INFERENCE HEURISTICS
########################################

Brand / platform detection — Use any of:

Logo shapes / colours (e.g. Meta blue, Instagram purple gradient).

Phrases like “Who saw your ad”, “Ads Manager”, “Campaign — …”.

Product names (“Grok”, “Reels”, “Sponsored”).

Contact e-mails — If brand domain is meta.com, generate variants such as support@meta.com, ads-support@meta.com, analytics-qa@meta.com.

Responsible party mapping —

Data-visualisation bug → “<Brand> Ads Insights Engineering & Data QA Team”

Form submit error → “<Brand> Web Growth Engineering”

Physical litter → “<Municipality> Public Works”

Percentage or sum anomalies — Always state the exact numbers in the summary.

########################################

5. EXEMPLARS (now two examples)
########################################

Example A - Grok early-access form
INPUT: screenshot showing an early-access form for "Grok for Business" returning "Error submitting form".
TARGET_OUTPUT:

json
Copy
{
  "issue_title": "Broken Form Submission on Grok for Business",
  "summary": "The early-access request form returns a generic submission error after 3 mandatory fields are completed, blocking lead capture.",
  "classification": "Digital Waste",
  "user_info": {
      "name": "Boris Mamlyuk",
      "email": "b@stxn.io",
      "company": "Smart Transactions Corp.",
      "role": "CEO",
      "company_size": "11-50"
  },
  "location": "x.ai/grok",
  "responsible_party": "x.ai Web Growth Engineering",
  "inferred_contact_emails": ["support@x.ai", "web@x.ai", "info@x.ai"],
  "suggested_remediation": [
      "Reproduce the POST in dev tools and capture server response.",
      "Examine backend logs for 4xx/5xx anomalies linked to the endpoint.",
      "Add field-level validation to replace the generic banner.",
      "Notify sign-ups once fixed and consider an alternate email form."
  ]
}
Example B - Gender breakdown > 100%
INPUT: screenshot reading "Who saw your ad - Gender - 0% Men • 101.6% Women" with an Instagram-purple doughnut chart.
TARGET_OUTPUT:

json
Copy
{
  "issue_title": "Ad Analytics Gender Breakdown Exceeds 100 %",
  "summary": "The insights widget displays 0% men and 101.6% women, so demographics total 101.6%. This indicates a percentage-calculation defect in the Meta Ads analytics pipeline.",
  "classification": "Digital Waste",
  "user_info": {
      "name": null,
      "email": null,
      "company": null,
      "role": null,
      "company_size": null
  },
  "location": "Meta / Instagram Ads Insights UI",
  "responsible_party": "Meta Ads Insights Engineering & Data QA Team",
  "inferred_contact_emails": [
      "ads-support@meta.com",
      "analytics-qa@meta.com",
      "support@fb.com"
  ],
  "suggested_remediation": [
      "Audit the aggregation query to ensure gender percentages are normalised to 100%.",
      "Verify rounding rules and apply compensating adjustments before display.",
      "Ship a unit test that fails if demographic sums deviate from 100 ± 0.1 %.",
      "Back-fill historical insight records and email affected advertisers once corrected."
  ]
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
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type ClassificationResult struct {
	DigitalProbability float64 `json:"digital_probability"`
	SiteURL            string  `json:"site_url"`
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
		fmt.Println("Usage: go run main.go <image_path> <description>")
		fmt.Println("Example: go run main.go ./image.jpg \"description phrase\"")
		return
	}

	imagePath := os.Args[1]
	description := os.Args[2]

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

	// Call API with the prompt
	fmt.Println("Analyzing with the provided prompt...")
	analysisResponse, err := callOpenAI(openAIKey, base64Image, systemPrompt, description)
	if err != nil {
		fmt.Printf("Error in analysis call: %v\n", err)
		return
	}

	fmt.Printf("Analysis response: %s\n", analysisResponse)
}

// callOpenAI makes a call to OpenAI API with the given prompt and image
func callOpenAI(apiKey, base64Image, prompt, description string) (string, error) {
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

	descriptionPrompt := TextContent{
		Type: "text",
		Text: description,
	}

	reqBody := ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{
				Role: "system",
				Content: []any{
					textPrompt,
				},
			},
			{
				Role: "user",
				Content: []any{
					imagePrompt,
					descriptionPrompt,
				},
			},
		},
		Temperature: 0.2,
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
