package service

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"report-analyze-pipeline/database"
)

var (
	htmlTagPattern      = regexp.MustCompile(`(?s)<[^>]*>`)
	whitespacePattern   = regexp.MustCompile(`\s+`)
	lineBreakTagPattern = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</blockquote>`)
)

type xOEmbedResponse struct {
	AuthorName string `json:"author_name"`
	AuthorURL  string `json:"author_url"`
	HTML       string `json:"html"`
}

func (s *Service) enrichAnalysisInput(report *database.Report, baseInput string) string {
	shareContext, err := fetchShareContext(report)
	if err != nil {
		log.Printf("Report %d: failed to fetch share context for %q: %v", report.Seq, report.SourceURL, err)
	}

	parts := make([]string, 0, 3)
	if trimmed := strings.TrimSpace(baseInput); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if shareContext != "" {
		parts = append(parts, shareContext)
	}
	if shouldWarnAboutThinEvidence(report, shareContext) {
		parts = append(parts, "Evidence quality note: there is no attached image and no retrievable remote post/page content. Do not invent specific bug details, metrics, IDs, or impacted products. If evidence is insufficient, say so clearly and keep the analysis generic.")
	}
	return strings.Join(parts, "\n\n")
}

func shouldWarnAboutThinEvidence(report *database.Report, shareContext string) bool {
	if len(strings.TrimSpace(shareContext)) > 0 {
		return false
	}
	if strings.TrimSpace(report.SharedText) != "" {
		return false
	}
	description := strings.TrimSpace(report.Description)
	if description == "" {
		return true
	}
	return strings.EqualFold(description, "Human report submission")
}

func fetchShareContext(report *database.Report) (string, error) {
	sourceURL := strings.TrimSpace(report.SourceURL)
	if sourceURL == "" {
		return "", nil
	}

	parsed, err := neturl.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("invalid source url: %w", err)
	}

	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	switch host {
	case "x.com", "twitter.com":
		return fetchXOEmbedContext(sourceURL)
	default:
		return "", nil
	}
}

func fetchXOEmbedContext(sourceURL string) (string, error) {
	query := neturl.Values{}
	query.Set("url", sourceURL)
	query.Set("omit_script", "true")
	query.Set("dnt", "true")

	endpoint := "https://publish.twitter.com/oembed?" + query.Encode()
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CleanAppAnalyzer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("oembed status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload xOEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	postText := extractTextFromOEmbedHTML(payload.HTML)
	if postText == "" {
		return "", nil
	}

	parts := []string{"Fetched X post context:"}
	if author := strings.TrimSpace(payload.AuthorName); author != "" {
		parts = append(parts, "Author: "+author)
	}
	if authorURL := strings.TrimSpace(payload.AuthorURL); authorURL != "" {
		parts = append(parts, "Author profile: "+authorURL)
	}
	parts = append(parts, "Post text: "+postText)
	return strings.Join(parts, "\n"), nil
}

func extractTextFromOEmbedHTML(fragment string) string {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return ""
	}

	normalized := lineBreakTagPattern.ReplaceAllString(fragment, "\n")
	normalized = htmlTagPattern.ReplaceAllString(normalized, " ")
	normalized = html.UnescapeString(normalized)

	lines := strings.Split(normalized, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = whitespacePattern.ReplaceAllString(strings.TrimSpace(line), " ")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "— ") || strings.HasPrefix(line, "&mdash; ") {
			continue
		}
		cleaned = append(cleaned, line)
	}

	return strings.Join(cleaned, "\n")
}
