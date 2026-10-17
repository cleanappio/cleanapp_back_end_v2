package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cleanapp-common/evidence"
	"report-analyze-pipeline/llm"
)

const promptSystem = llm.ImageAnalysisPrompt
const promptSystemTextOnly = llm.TextAnalysisPrompt

type inlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
}

type content struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	ResponseMimeType string `json:"response_mime_type,omitempty"`
}

type geminiRequest struct {
	GenerationConfig generationConfig `json:"generationConfig,omitempty"`
	Contents         []content        `json:"contents"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text,omitempty"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *Client) SourceName() string {
	return "Gemini"
}

func (c *Client) AnalyzeImage(imageData []byte, description string) (string, error) {
	systemPrompt := promptSystem
	if len(imageData) == 0 {
		systemPrompt = promptSystemTextOnly
	}

	parts := []part{{Text: systemPrompt}}
	if description != "" {
		parts = append(parts, part{Text: description})
	}
	if len(imageData) > 0 {
		// Do not attach "placeholder" or invalid image blobs; Gemini will 400 and the message will retry forever.
		mimeType := http.DetectContentType(imageData)
		if strings.HasPrefix(mimeType, "image/") {
			if !shouldSkipImageAttachment(imageData, mimeType) {
				parts = append(parts, part{
					InlineData: &inlineData{
						MimeType: mimeType,
						Data:     base64.StdEncoding.EncodeToString(imageData),
					},
				})
			}
		}
	}

	reqBody := geminiRequest{
		Contents: []content{
			{
				Role:  "user",
				Parts: parts,
			},
		},
	}

	return c.generateContent(reqBody)
}

// LocationContext contains location information from OSM for physical report analysis
type LocationContext = llm.LocationContext

// AnalyzeImageWithLocation analyzes an image with additional location context for physical reports
func (c *Client) AnalyzeImageWithLocation(imageData []byte, description string, locCtx *LocationContext) (string, error) {
	locationContextStr := llm.LocationPrompt(locCtx)

	parts := []part{{Text: promptSystem + locationContextStr}}
	if description != "" {
		parts = append(parts, part{Text: description})
	}
	if len(imageData) > 0 {
		// Do not attach "placeholder" or invalid image blobs; Gemini will 400 and the message will retry forever.
		mimeType := http.DetectContentType(imageData)
		if strings.HasPrefix(mimeType, "image/") {
			if !shouldSkipImageAttachment(imageData, mimeType) {
				parts = append(parts, part{
					InlineData: &inlineData{
						MimeType: mimeType,
						Data:     base64.StdEncoding.EncodeToString(imageData),
					},
				})
			}
		}
	}

	reqBody := geminiRequest{
		Contents: []content{
			{
				Role:  "user",
				Parts: parts,
			},
		},
	}

	return c.generateContent(reqBody)
}

func shouldSkipImageAttachment(imageData []byte, mimeType string) bool {
	return llm.ShouldSkipImageAttachment(imageData, mimeType)
}

func (c *Client) AnalyzeEvidence(original, entity string) (evidence.Fields, error) {
	return c.AnalyzeEvidenceImage(nil, original, entity)
}
func (c *Client) AnalyzeEvidenceImage(image []byte, original, entity string) (evidence.Fields, error) {
	return evidence.NewGeminiClassifier(c.apiKey, c.model, c.http).AnalyzeEvidenceImage(image, original, entity)
}

func (c *Client) TranslateAnalysis(jsonText, targetLanguage string) (string, error) {
	prompt := fmt.Sprintf("Please translate values in the following JSON to %s. Translate all values except the field classification.\n\n%s", targetLanguage, jsonText)
	reqBody := geminiRequest{
		Contents: []content{
			{
				Role: "user",
				Parts: []part{
					{Text: prompt},
				},
			},
		},
	}
	return c.generateContent(reqBody)
}

func (c *Client) generateContent(body geminiRequest) (string, error) {
	// try v1beta first, then v1
	endpoints := []string{
		fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.model),
		fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", c.model),
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for attempt, ep := range endpoints {
		startedAt := time.Now()
		apiVersion := "v1beta"
		if attempt == 1 {
			apiVersion = "v1"
		}
		req, err := http.NewRequest("POST", ep, bytes.NewBuffer(data))
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		// Keep credentials out of URL-bearing transport errors and access logs.
		req.Header.Set("x-goog-api-key", c.apiKey)
		resp, err := c.http.Do(req)
		if err != nil {
			log.Printf("gemini request model=%s api_version=%s duration_ms=%d status=0 outcome=transport_error", c.model, apiVersion, time.Since(startedAt).Milliseconds())
			lastErr = fmt.Errorf("failed to send request: %w", err)
			continue
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		log.Printf("gemini request model=%s api_version=%s duration_ms=%d status=%d", c.model, apiVersion, time.Since(startedAt).Milliseconds(), resp.StatusCode)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Provider messages can echo submitted data; retain status without logging report content.
			var providerError struct {
				Error struct {
					Status string `json:"status"`
				} `json:"error"`
			}
			_ = json.Unmarshal(bodyBytes, &providerError)
			lastErr = fmt.Errorf("API error (status %d, code %s)", resp.StatusCode, providerError.Error.Status)
			// retry next endpoint if available
			continue
		}
		var gr geminiResponse
		if err := json.Unmarshal(bodyBytes, &gr); err != nil {
			lastErr = fmt.Errorf("failed to parse response: %w", err)
			continue
		}
		if len(gr.Candidates) == 0 {
			lastErr = fmt.Errorf("no candidates in response")
			continue
		}
		// find first text part
		for _, p := range gr.Candidates[0].Content.Parts {
			if p.Text != "" {
				return p.Text, nil
			}
		}
		lastErr = fmt.Errorf("no text part in response")
	}
	return "", lastErr
}
