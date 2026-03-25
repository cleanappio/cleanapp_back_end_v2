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
	xStatusIDPattern    = regexp.MustCompile(`(?i)(?:/status/|/statuses/|post/page from x\.com:\s*)(\d{8,32})`)
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
		return fetchXContext(report, sourceURL)
	default:
		return "", nil
	}
}

func fetchXContext(report *database.Report, sourceURL string) (string, error) {
	candidates := buildXOEmbedCandidates(report, sourceURL)
	var failures []string

	for _, candidate := range candidates {
		context, err := fetchXOEmbedContext(candidate)
		if err == nil && strings.TrimSpace(context) != "" {
			log.Printf("Report %d: fetched X share context via %q", report.Seq, candidate)
			return context, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s => %v", candidate, err))
			continue
		}
		failures = append(failures, fmt.Sprintf("%s => empty post text", candidate))
	}

	if len(failures) > 0 {
		return "", fmt.Errorf("x share context unavailable (%s)", strings.Join(failures, "; "))
	}
	return "", nil
}

func buildXOEmbedCandidates(report *database.Report, sourceURL string) []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		candidates = append(candidates, raw)
	}

	add(sourceURL)

	if parsed, err := neturl.Parse(sourceURL); err == nil {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		add(parsed.String())
	}

	if statusID := extractXStatusID(sourceURL, report.Description, report.SharedText); statusID != "" {
		add("https://x.com/i/status/" + statusID)
		add("https://twitter.com/i/status/" + statusID)
	}

	return candidates
}

func extractXStatusID(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		matches := xStatusIDPattern.FindStringSubmatch(candidate)
		if len(matches) == 2 {
			return matches[1]
		}
	}
	return ""
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
		return "", fmt.Errorf("oembed returned empty post text")
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
