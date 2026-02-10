package stubllm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"report-analyze-pipeline/parser"
)

// Client is a deterministic, no-network LLM stub intended for CI and local end-to-end tests.
// It returns schema-valid JSON so downstream parsing + DB writes exercise the full pipeline.
type Client struct{}

func NewClient() *Client { return &Client{} }

func (c *Client) SourceName() string { return "Stub" }

func (c *Client) AnalyzeImage(imageData []byte, description string) (string, error) {
	// Make output deterministic per-input so the pipeline is stable in CI.
	sum := sha256.Sum256(append([]byte(description), imageData...))
	short := hex.EncodeToString(sum[:8])

	out := map[string]any{
		"title":                   fmt.Sprintf("CI Stub Analysis (%s)", short),
		"description":             fmt.Sprintf("Stubbed analysis for: %s", truncate(description, 120)),
		"classification":          "physical",
		"user_info":               map[string]any{"name": "", "email": "", "company": "", "role": "", "company_size": ""},
		"location":                "",
		"brand_name":              "",
		"responsible_party":       "",
		"inferred_contact_emails": []string{},
		"suggested_remediation":   []string{"Investigate", "Remediate", "Close out"},
		"litter_probability":      0.5,
		"hazard_probability":      0.1,
		// Note the historical spelling used by the parser schema.
		"digital_bug_probabilty": 0.0,
		"severity_level":         0.2,
		"legal_risk_estimate":    "low",
		"is_valid":               true,
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) TranslateAnalysis(jsonText, targetLanguage string) (string, error) {
	// Keep it simple and schema-valid: round-trip JSON and annotate title/description.
	clean := parser.ExtractJSONFromMarkdown(jsonText)
	var obj map[string]any
	if err := json.Unmarshal([]byte(clean), &obj); err != nil {
		// If input isn't valid JSON, return it unchanged to preserve caller error behavior.
		return jsonText, nil
	}

	if t, ok := obj["title"].(string); ok && t != "" {
		obj["title"] = fmt.Sprintf("[%s] %s", targetLanguage, t)
	}
	if d, ok := obj["description"].(string); ok && d != "" {
		obj["description"] = fmt.Sprintf("[%s] %s", targetLanguage, d)
	}

	b, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}
