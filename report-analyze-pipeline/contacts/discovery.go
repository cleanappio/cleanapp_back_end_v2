package contacts

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// LinkedInProfile represents a scraped LinkedIn profile
type LinkedInProfile struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Company    string `json:"company"`
	ProfileURL string `json:"profile_url"`
}

// GitHubContributor represents a GitHub contributor
type GitHubContributor struct {
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Company string `json:"company,omitempty"`
	Twitter string `json:"twitter_username,omitempty"`
}

// TwitterProfile represents a Twitter/X profile
type TwitterProfile struct {
	Handle string `json:"handle"`
	Name   string `json:"name"`
}

// SearchLinkedInViaGoogle searches for LinkedIn profiles using Google dork
// Uses: site:linkedin.com/in "Company" "Title"
func (s *ContactService) SearchLinkedInViaGoogle(company, title string) ([]LinkedInProfile, error) {
	// Build Google search query
	query := fmt.Sprintf(`site:linkedin.com/in "%s" "%s"`, company, title)
	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s&num=10", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use a desktop browser user agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Extract LinkedIn profile URLs from Google results
	return parseLinkedInFromGoogle(string(body), company), nil
}

// parseLinkedInFromGoogle extracts LinkedIn profiles from Google search results HTML
func parseLinkedInFromGoogle(html, company string) []LinkedInProfile {
	var profiles []LinkedInProfile
	seen := make(map[string]bool)

	// Regex to find LinkedIn profile URLs
	// Format: https://www.linkedin.com/in/username or https://linkedin.com/in/username
	linkedinRegex := regexp.MustCompile(`https?://(?:www\.)?linkedin\.com/in/([a-zA-Z0-9\-]+)`)

	matches := linkedinRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			username := match[1]
			profileURL := "https://www.linkedin.com/in/" + username

			if !seen[username] {
				seen[username] = true
				profiles = append(profiles, LinkedInProfile{
					Name:       formatNameFromUsername(username),
					ProfileURL: profileURL,
					Company:    company,
				})
			}
		}
	}

	return profiles
}

// formatNameFromUsername converts a LinkedIn username to a display name
// e.g., "john-doe-123abc" -> "John Doe"
func formatNameFromUsername(username string) string {
	// Remove trailing numeric/hash suffixes
	parts := strings.Split(username, "-")
	var nameParts []string

	for _, part := range parts {
		// Skip parts that are mostly numbers (likely IDs)
		if len(part) > 0 && !isNumeric(part) {
			nameParts = append(nameParts, strings.Title(part))
		}
		// Stop after 2-3 name parts (first, last, maybe middle)
		if len(nameParts) >= 3 {
			break
		}
	}

	return strings.Join(nameParts, " ")
}

// isNumeric checks if a string is mostly numeric
func isNumeric(s string) bool {
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return float64(digits)/float64(len(s)) > 0.5
}

// SearchGitHubContributors finds contributors for a GitHub repository
func (s *ContactService) SearchGitHubContributors(owner, repo string) ([]GitHubContributor, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contributors?per_page=10", owner, repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "CleanApp/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contributors: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}

	var contributors []GitHubContributor
	if err := json.NewDecoder(resp.Body).Decode(&contributors); err != nil {
		return nil, fmt.Errorf("failed to decode contributors: %w", err)
	}

	// Fetch additional user details for top contributors
	for i := range contributors {
		if i >= 5 { // Limit to top 5
			break
		}
		details, err := s.getGitHubUserDetails(contributors[i].Login)
		if err == nil {
			contributors[i].Name = details.Name
			contributors[i].Email = details.Email
			contributors[i].Company = details.Company
			contributors[i].Twitter = details.Twitter
		}
	}

	return contributors, nil
}

// getGitHubUserDetails fetches detailed info for a GitHub user
func (s *ContactService) getGitHubUserDetails(username string) (*GitHubContributor, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s", username)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "CleanApp/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var user GitHubContributor
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// SearchTwitterHandles searches for Twitter/X handles for a company or person
func (s *ContactService) SearchTwitterHandles(companyName string) ([]TwitterProfile, error) {
	// Try to find Twitter handles via Google search
	query := fmt.Sprintf(`site:twitter.com OR site:x.com "%s"`, companyName)
	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s&num=10", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return parseTwitterFromGoogle(string(body)), nil
}

// parseTwitterFromGoogle extracts Twitter handles from Google search results
func parseTwitterFromGoogle(html string) []TwitterProfile {
	var profiles []TwitterProfile
	seen := make(map[string]bool)

	// Regex to find Twitter/X handles
	twitterRegex := regexp.MustCompile(`https?://(?:www\.)?(?:twitter|x)\.com/([a-zA-Z0-9_]+)`)

	matches := twitterRegex.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 1 {
			handle := strings.ToLower(match[1])

			// Skip common non-profile URLs
			if handle == "home" || handle == "search" || handle == "explore" ||
				handle == "notifications" || handle == "messages" || handle == "i" ||
				handle == "settings" || handle == "intent" || handle == "share" {
				continue
			}

			if !seen[handle] {
				seen[handle] = true
				profiles = append(profiles, TwitterProfile{
					Handle: "@" + handle,
				})
			}
		}
	}

	return profiles
}

// DiscoverContactsForBrand performs full discovery for a brand
// This is the main entry point for Phase 2 discovery
func (s *ContactService) DiscoverContactsForBrand(brandName, domain string) ([]Contact, error) {
	var discovered []Contact

	log.Printf("Starting contact discovery for brand %q domain %q", brandName, domain)

	// 1. Search LinkedIn for executives
	titles := []string{"CEO", "CTO", "VP Engineering", "Product Manager", "Head of"}
	for _, title := range titles {
		profiles, err := s.SearchLinkedInViaGoogle(brandName, title)
		if err != nil {
			log.Printf("LinkedIn search failed for %s %s: %v", brandName, title, err)
			continue
		}

		for _, p := range profiles {
			// Infer email from name and domain
			emails := InferEmailsFromName(p.Name, domain)
			var email string
			if len(emails) > 0 {
				email = emails[0] // Use first pattern
			}

			discovered = append(discovered, Contact{
				BrandName:    brandName,
				ContactName:  p.Name,
				ContactTitle: title,
				Email:        email,
				LinkedInURL:  p.ProfileURL,
				Source:       "linkedin",
			})
		}
	}

	// 2. Search Twitter/X for brand handles
	twitterProfiles, err := s.SearchTwitterHandles(brandName)
	if err != nil {
		log.Printf("Twitter search failed for %s: %v", brandName, err)
	} else {
		for _, t := range twitterProfiles {
			// Try to find if we already have a contact with this handle
			found := false
			for i := range discovered {
				if discovered[i].TwitterHandle == "" {
					discovered[i].TwitterHandle = t.Handle
					found = true
					break
				}
			}
			if !found && len(twitterProfiles) <= 3 {
				// Add as new contact
				discovered = append(discovered, Contact{
					BrandName:     brandName,
					TwitterHandle: t.Handle,
					Source:        "twitter",
				})
			}
		}
	}

	log.Printf("Discovered %d contacts for brand %q", len(discovered), brandName)
	return discovered, nil
}

// DiscoverAndSaveContactsForBrand discovers and saves contacts for a brand
func (s *ContactService) DiscoverAndSaveContactsForBrand(brandName, domain string) error {
	contacts, err := s.DiscoverContactsForBrand(brandName, domain)
	if err != nil {
		return err
	}

	for _, c := range contacts {
		c.BrandName = strings.ToLower(c.BrandName)
		if err := s.SaveContact(&c); err != nil {
			log.Printf("Failed to save discovered contact: %v", err)
		}
	}

	return nil
}

// GetGitHubRepoForBrand maps brand names to their main GitHub repos
func GetGitHubRepoForBrand(brandName string) (owner, repo string) {
	repos := map[string][2]string{
		"openai":    {"openai", "openai-python"},
		"anthropic": {"anthropics", "anthropic-sdk-python"},
		"google":    {"google", "generative-ai-python"},
		"meta":      {"facebookresearch", "llama"},
		"microsoft": {"microsoft", "vscode"},
		"github":    {"github", "docs"},
		"vercel":    {"vercel", "next.js"},
		"stripe":    {"stripe", "stripe-python"},
		"shopify":   {"Shopify", "shopify-api-ruby"},
	}

	if r, ok := repos[strings.ToLower(brandName)]; ok {
		return r[0], r[1]
	}
	return "", ""
}
