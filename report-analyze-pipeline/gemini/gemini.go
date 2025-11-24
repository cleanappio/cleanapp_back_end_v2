package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const promptSystem = `
You are **CleanApp Analyzer**, a vision-enabled expert that converts screenshots or photos of broken physical or digital systems into a high-value metadata report.

########################################
# 1. MISSION
########################################
For every input (images ± text) you MUST:

Step 1: ========: Detect the input type. If the image contains a photo or a screenshot of the computer monitor or a mobile phone then consider it a digital input. If the image contains any physical object then consider it a physical input.
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
  "title":            "<headline>",
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

Phrases like “Who saw your ad”, “Ads Manager”, “Campaign — …”.

Product names (“Grok”, “Reels”, “Sponsored”).

Contact e-mails — If brand domain is meta.com, generate variants such as support@meta.com, ads-support@meta.com, analytics-qa@meta.com.

Responsible party mapping —

Data-visualisation bug → “<Brand> Ads Insights Engineering & Data QA Team”

Form submit error → “<Brand> Web Growth Engineering”

Physical litter → “<Municipality> Public Works”

Percentage or sum anomalies — Always state the exact numbers in the summary.
`

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
		http:   &http.Client{},
	}
}

func (c *Client) SourceName() string {
	return "Gemini"
}

func (c *Client) AnalyzeImage(imageData []byte, description string) (string, error) {
	parts := []part{{Text: promptSystem}}
	if description != "" {
		parts = append(parts, part{Text: description})
	}
	if len(imageData) > 0 {
		parts = append(parts, part{
			InlineData: &inlineData{
				MimeType: "image/jpeg",
				Data:     base64.StdEncoding.EncodeToString(imageData),
			},
		})
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
		fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey),
		fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent?key=%s", c.model, c.apiKey),
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for _, ep := range endpoints {
		req, err := http.NewRequest("POST", ep, bytes.NewBuffer(data))
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			continue
		}
		defer resp.Body.Close()
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
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
