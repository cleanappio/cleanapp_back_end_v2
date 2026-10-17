package claude

import (
	"cleanapp-common/anthropic"
	"cleanapp-common/evidence"
	"context"
	"fmt"
	"net/http"
	"report-analyze-pipeline/llm"
	"strings"
)

type Client struct {
	api      *anthropic.Client
	evidence *evidence.Provider
}

func NewClient(key, model string) *Client { return newClient(key, model, nil) }
func newClient(key, model string, httpClient *http.Client) *Client {
	return &Client{api: anthropic.NewClient(key, model, httpClient), evidence: evidence.NewClaudeClassifier(key, model, httpClient)}
}
func (c *Client) SourceName() string { return "Claude" }
func (c *Client) AnalyzeImage(image []byte, description string) (string, error) {
	return c.analyze(image, description, nil)
}
func (c *Client) AnalyzeImageWithLocation(image []byte, description string, location *llm.LocationContext) (string, error) {
	return c.analyze(image, description, location)
}
func (c *Client) analyze(image []byte, description string, location *llm.LocationContext) (string, error) {
	mime := http.DetectContentType(image)
	if !strings.HasPrefix(mime, "image/") || llm.ShouldSkipImageAttachment(image, mime) {
		image = nil
	}
	prompt := llm.TextAnalysisPrompt
	if len(image) > 0 {
		prompt = llm.ImageAnalysisPrompt
	}
	return c.api.Generate(context.Background(), anthropic.Input{System: prompt + llm.LocationPrompt(location), Text: description, Image: image, JSONSchema: llm.AnalysisSchema()})
}
func (c *Client) TranslateAnalysis(text, language string) (string, error) {
	prompt := fmt.Sprintf("Please translate values in the following JSON to %s. Translate all values except the field classification.\n\n%s", language, text)
	return c.api.Generate(context.Background(), anthropic.Input{Text: prompt, JSONSchema: llm.AnalysisSchema()})
}
func (c *Client) AnalyzeEvidence(original, entity string) (evidence.Fields, error) {
	return c.evidence.AnalyzeEvidence(original, entity)
}
func (c *Client) AnalyzeEvidenceImage(image []byte, original, entity string) (evidence.Fields, error) {
	return c.evidence.AnalyzeEvidenceImage(image, original, entity)
}
func (c *Client) AnalyzeOriginalEvidence(original evidence.Original, entity string) (evidence.Fields, error) {
	return c.evidence.AnalyzeOriginalEvidence(original, entity)
}
