package osm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Note: OverpassBaseURL is declared in osm.go

// googleSearchDailyCounter tracks daily API usage to prevent cost overruns
var googleSearchDailyCounter = struct {
	sync.Mutex
	count int
	date  string
}{}

// DefaultGoogleSearchDailyLimit is the default max searches per day (can be overridden by GOOGLE_SEARCH_DAILY_LIMIT env var)
const DefaultGoogleSearchDailyLimit = 1000

// POI represents a Point of Interest from Overpass
type POI struct {
	ID           int64             `json:"id"`
	Type         string            `json:"type"` // node, way, relation
	Name         string            `json:"name"`
	Operator     string            `json:"operator"`
	Website      string            `json:"website"`
	ContactEmail string            `json:"contact_email"`
	Amenity      string            `json:"amenity"`
	Building     string            `json:"building"`
	Tags         map[string]string `json:"tags"`
	Lat          float64           `json:"lat"`
	Lon          float64           `json:"lon"`
}

// OverpassResponse is the response from the Overpass API
type OverpassResponse struct {
	Elements []OverpassElement `json:"elements"`
}

// OverpassElement represents an element from Overpass
type OverpassElement struct {
	Type   string            `json:"type"`
	ID     int64             `json:"id"`
	Lat    float64           `json:"lat,omitempty"`
	Lon    float64           `json:"lon,omitempty"`
	Center *OverpassCenter   `json:"center,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

// OverpassCenter is the center of a way/relation
type OverpassCenter struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// QueryNearbyPOIs queries Overpass for POIs within a given radius
func (c *Client) QueryNearbyPOIs(lat, lon float64, radiusMeters int) ([]POI, error) {
	c.enforceRateLimit()

	// Build Overpass QL query for nearby amenities, buildings, etc.
	query := fmt.Sprintf(`
[out:json][timeout:25];
(
  // Universities, schools, colleges
  nwr["amenity"~"university|college|school"](around:%d,%f,%f);
  // Hospitals, clinics
  nwr["amenity"~"hospital|clinic"](around:%d,%f,%f);
  // Offices
  nwr["office"](around:%d,%f,%f);
  // Named buildings
  nwr["building"]["name"](around:%d,%f,%f);
  // Public facilities
  nwr["amenity"~"library|community_centre|townhall|courthouse"](around:%d,%f,%f);
  // Shopping
  nwr["shop"](around:%d,%f,%f);
  // Tourism
  nwr["tourism"~"hotel|museum|attraction"](around:%d,%f,%f);
  // Transport
  nwr["aeroway"="terminal"](around:%d,%f,%f);
  nwr["railway"="station"](around:%d,%f,%f);
);
out center;
`, 
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
		radiusMeters, lat, lon,
	)

	// URL encode the query
	reqURL := fmt.Sprintf("%s?data=%s", OverpassBaseURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Overpass request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Overpass request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Overpass returned status %d: %s", resp.StatusCode, string(body))
	}

	var overpassResp OverpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&overpassResp); err != nil {
		return nil, fmt.Errorf("failed to decode Overpass response: %w", err)
	}

	// Convert to POI structs
	var pois []POI
	for _, elem := range overpassResp.Elements {
		poi := POI{
			ID:   elem.ID,
			Type: elem.Type,
			Tags: elem.Tags,
		}

		// Get coordinates
		if elem.Lat != 0 && elem.Lon != 0 {
			poi.Lat = elem.Lat
			poi.Lon = elem.Lon
		} else if elem.Center != nil {
			poi.Lat = elem.Center.Lat
			poi.Lon = elem.Center.Lon
		}

		// Extract common tags
		if elem.Tags != nil {
			poi.Name = elem.Tags["name"]
			poi.Operator = elem.Tags["operator"]
			poi.Amenity = elem.Tags["amenity"]
			poi.Building = elem.Tags["building"]
			
			// Extract contact info
			if email, ok := elem.Tags["contact:email"]; ok {
				poi.ContactEmail = email
			} else if email, ok := elem.Tags["email"]; ok {
				poi.ContactEmail = email
			}
			
			// Extract website
			if website, ok := elem.Tags["website"]; ok {
				poi.Website = website
			} else if website, ok := elem.Tags["contact:website"]; ok {
				poi.Website = website
			} else if website, ok := elem.Tags["operator:website"]; ok {
				poi.Website = website
			}
		}

		// Only include POIs with names
		if poi.Name != "" || poi.Operator != "" {
			pois = append(pois, poi)
		}
	}

	return pois, nil
}

// HierarchyLevel represents a level in the place hierarchy
type HierarchyLevel struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // building, campus, university, city, etc.
	Domain      string `json:"domain"`
	Operator    string `json:"operator"`
	ContactEmail string `json:"contact_email"`
}

// GetLocationHierarchy returns a hierarchy of locations from specific to general
func (c *Client) GetLocationHierarchy(ctx *LocationContext) []HierarchyLevel {
	var hierarchy []HierarchyLevel

	// Level 1: Specific location (building/department)
	if ctx.PrimaryName != "" && ctx.PrimaryName != ctx.ParentOrg {
		hierarchy = append(hierarchy, HierarchyLevel{
			Name:         ctx.PrimaryName,
			Type:         ctx.LocationType,
			Domain:       ctx.Domain,
			ContactEmail: ctx.ContactEmail,
		})
	}

	// Level 2: Parent organization (e.g., campus/university)
	if ctx.ParentOrg != "" {
		parentDomain := inferDomainFromOrgName(ctx.ParentOrg)
		hierarchy = append(hierarchy, HierarchyLevel{
			Name:     ctx.ParentOrg,
			Type:     "organization",
			Domain:   parentDomain,
			Operator: ctx.Operator,
		})
	}

	// Level 3: Operator (if different from parent)
	if ctx.Operator != "" && ctx.Operator != ctx.ParentOrg {
		opDomain := inferDomainFromOrgName(ctx.Operator)
		hierarchy = append(hierarchy, HierarchyLevel{
			Name:   ctx.Operator,
			Type:   "operator",
			Domain: opDomain,
		})
	}

	// Level 4: City government
	if ctx.Address.City != "" {
		cityDomain := inferCityGovDomain(ctx.Address.City, ctx.Address.State, ctx.Address.CountryCode)
		if cityDomain != "" {
			hierarchy = append(hierarchy, HierarchyLevel{
				Name:   ctx.Address.City,
				Type:   "city",
				Domain: cityDomain,
			})
		}
	}

	return hierarchy
}

// inferDomainFromOrgName attempts to infer a domain from an organization name
func inferDomainFromOrgName(orgName string) string {
	name := strings.ToLower(orgName)
	
	// Common university patterns
	if strings.Contains(name, "university of california") {
		// Extract campus name: "University of California, Los Angeles" -> "ucla.edu"
		parts := strings.Split(name, ",")
		if len(parts) > 1 {
			campus := strings.TrimSpace(parts[1])
			abbreviations := map[string]string{
				"los angeles": "ucla.edu",
				"berkeley":    "berkeley.edu",
				"san diego":   "ucsd.edu",
				"davis":       "ucdavis.edu",
				"irvine":      "uci.edu",
				"santa barbara": "ucsb.edu",
				"santa cruz":  "ucsc.edu",
				"riverside":   "ucr.edu",
				"merced":      "ucmerced.edu",
			}
			if domain, ok := abbreviations[campus]; ok {
				return domain
			}
		}
	}
	
	// Generic university: try to form acronym.edu
	if strings.Contains(name, "university") || strings.Contains(name, "college") {
		// Remove common words and form acronym
		words := strings.Fields(name)
		var acronym strings.Builder
		for _, word := range words {
			if word != "the" && word != "of" && word != "and" && word != "at" &&
				word != "university" && word != "college" && word != "state" {
				if len(word) > 0 {
					acronym.WriteByte(word[0])
				}
			}
		}
		if acronym.Len() >= 2 {
			return strings.ToLower(acronym.String()) + ".edu"
		}
	}

	return ""
}

// inferCityGovDomain attempts to infer a city government domain
func inferCityGovDomain(city, state, countryCode string) string {
	if countryCode != "us" && countryCode != "US" && countryCode != "" {
		return "" // Only handle US cities for now
	}

	// Normalize city name
	cityNorm := strings.ToLower(city)
	cityNorm = strings.ReplaceAll(cityNorm, " ", "")
	cityNorm = strings.ReplaceAll(cityNorm, "-", "")
	
	// Try common patterns
	// Los Angeles -> lacity.gov, losangeles.gov
	// Santa Monica -> santamonica.gov, smgov.net
	
	// Default to city.gov pattern
	return cityNorm + ".gov"
}

// ValidEmail checks if an email address is syntactically valid
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ValidateEmail checks if an email is valid and not a placeholder
func ValidateEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	
	// Check syntax
	if !emailRegex.MatchString(email) {
		return false
	}
	
	// Filter obvious placeholders
	lower := strings.ToLower(email)
	placeholders := []string{
		"test@", "example@", "sample@", "demo@",
		"noreply@", "no-reply@", "donotreply@",
		"admin@localhost", "user@localhost",
		"@example.com", "@test.com", "@localhost",
	}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return false
		}
	}
	
	return true
}

// ValidateAndFilterEmails filters a list of emails to only valid ones
func ValidateAndFilterEmails(emails []string) []string {
	var valid []string
	seen := make(map[string]bool)
	
	for _, email := range emails {
		email = strings.TrimSpace(email)
		lower := strings.ToLower(email)
		
		if ValidateEmail(email) && !seen[lower] {
			valid = append(valid, email)
			seen[lower] = true
		}
	}
	
	return valid
}

// ScrapeEmailsFromWebsite attempts to extract email addresses from a website
func (c *Client) ScrapeEmailsFromWebsite(websiteURL string) ([]string, error) {
	if websiteURL == "" {
		return nil, nil
	}

	// Ensure URL has scheme
	if !strings.HasPrefix(websiteURL, "http://") && !strings.HasPrefix(websiteURL, "https://") {
		websiteURL = "https://" + websiteURL
	}

	// Create a client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", websiteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch website: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("website returned status %d", resp.StatusCode)
	}

	// Read body (limit to 1MB to avoid memory issues)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read website body: %w", err)
	}

	// Find all email addresses in the HTML
	return extractEmailsFromHTML(string(body)), nil
}

// extractEmailsFromHTML extracts email addresses from HTML content
func extractEmailsFromHTML(html string) []string {
	// Look for mailto: links first (most reliable)
	mailtoRegex := regexp.MustCompile(`mailto:([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)
	matches := mailtoRegex.FindAllStringSubmatch(html, -1)
	
	var emails []string
	seen := make(map[string]bool)
	
	for _, match := range matches {
		if len(match) > 1 {
			email := strings.ToLower(match[1])
			if !seen[email] && ValidateEmail(email) {
				emails = append(emails, email)
				seen[email] = true
			}
		}
	}
	
	// Also look for plain email addresses (but these may have more false positives)
	plainEmailRegex := regexp.MustCompile(`\b([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.(edu|gov|org|com|net))\b`)
	plainMatches := plainEmailRegex.FindAllStringSubmatch(html, -1)
	
	for _, match := range plainMatches {
		if len(match) > 1 {
			email := strings.ToLower(match[1])
			if !seen[email] && ValidateEmail(email) {
				emails = append(emails, email)
				seen[email] = true
			}
		}
	}
	
	return emails
}

// SearchLocationEmails searches Google Custom Search API for contact emails for a location
// This is a fallback when OSM/website scraping doesn't find emails
// Requires GOOGLE_SEARCH_API_KEY and GOOGLE_SEARCH_CX environment variables
// Rate limited to GOOGLE_SEARCH_DAILY_LIMIT per day (default 1000)
func (c *Client) SearchLocationEmails(locationName, city string) ([]string, error) {
	if locationName == "" {
		return nil, nil
	}

	// Get API credentials from environment
	apiKey := os.Getenv("GOOGLE_SEARCH_API_KEY")
	cseID := os.Getenv("GOOGLE_SEARCH_CX")
	
	if apiKey == "" || cseID == "" {
		// Fall back to no search if credentials not configured
		return nil, nil
	}

	// Check daily limit to prevent cost overruns
	dailyLimit := DefaultGoogleSearchDailyLimit
	if limitStr := os.Getenv("GOOGLE_SEARCH_DAILY_LIMIT"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			dailyLimit = parsed
		}
	}
	
	today := time.Now().UTC().Format("2006-01-02")
	googleSearchDailyCounter.Lock()
	if googleSearchDailyCounter.date != today {
		// Reset counter for new day
		googleSearchDailyCounter.date = today
		googleSearchDailyCounter.count = 0
		log.Printf("Google Search daily counter reset for %s", today)
	}
	if googleSearchDailyCounter.count >= dailyLimit {
		googleSearchDailyCounter.Unlock()
		log.Printf("Google Search daily limit (%d) reached, skipping search for %q", dailyLimit, locationName)
		return nil, nil
	}
	googleSearchDailyCounter.count++
	currentCount := googleSearchDailyCounter.count
	googleSearchDailyCounter.Unlock()
	
	log.Printf("Google Search API call %d/%d for location %q", currentCount, dailyLimit, locationName)

	c.enforceRateLimit()

	// Build search query: "Location Name" city contact email
	query := fmt.Sprintf(`"%s" %s contact email`, locationName, city)
	if city == "" {
		query = fmt.Sprintf(`"%s" contact email`, locationName)
	}
	
	// Use Google Custom Search JSON API
	searchURL := fmt.Sprintf(
		"https://www.googleapis.com/customsearch/v1?key=%s&cx=%s&q=%s&num=10",
		apiKey, cseID, url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Google Custom Search returned status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp GoogleSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	var emails []string
	
	// Extract emails from search result snippets
	for _, item := range searchResp.Items {
		// Check snippet for emails
		snippetEmails := extractEmailsFromHTML(item.Snippet)
		emails = append(emails, snippetEmails...)
		
		// Scrape the result URL for emails (limit to first 3)
		if len(emails) < 5 && item.Link != "" {
			scraped, err := c.ScrapeEmailsFromWebsite(item.Link)
			if err == nil {
				emails = append(emails, scraped...)
			}
		}
	}

	return ValidateAndFilterEmails(emails), nil
}

// GoogleSearchResponse is the response from Google Custom Search JSON API
type GoogleSearchResponse struct {
	Items []GoogleSearchItem `json:"items"`
}

// GoogleSearchItem represents a single search result
type GoogleSearchItem struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// extractWebsiteURLsFromGoogle extracts relevant website URLs from Google search results
func extractWebsiteURLsFromGoogle(html, locationName string) []string {
	var urls []string
	seen := make(map[string]bool)
	
	// Look for URLs in search results
	// Google often uses /url?q=https://... format
	urlRegex := regexp.MustCompile(`/url\?q=(https?://[^&"]+)`)
	matches := urlRegex.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			rawURL := match[1]
			// Decode URL
			decoded, err := url.QueryUnescape(rawURL)
			if err == nil {
				rawURL = decoded
			}
			
			// Filter out Google's own URLs and social media
			lower := strings.ToLower(rawURL)
			if strings.Contains(lower, "google.") ||
				strings.Contains(lower, "youtube.") ||
				strings.Contains(lower, "facebook.") ||
				strings.Contains(lower, "twitter.") ||
				strings.Contains(lower, "instagram.") ||
				strings.Contains(lower, "linkedin.") {
				continue
			}
			
			// Look for contact/about pages
			if !seen[rawURL] {
				seen[rawURL] = true
				// Prioritize contact pages
				if strings.Contains(lower, "contact") || 
					strings.Contains(lower, "about") ||
					strings.Contains(lower, "impressum") {
					urls = append([]string{rawURL}, urls...) // prepend
				} else {
					urls = append(urls, rawURL)
				}
			}
		}
	}
	
	// Limit results
	if len(urls) > 5 {
		urls = urls[:5]
	}
	
	return urls
}

// GenerateHierarchyEmails generates email addresses for each level of the hierarchy
func GenerateHierarchyEmails(hierarchy []HierarchyLevel) []string {
	var emails []string
	seen := make(map[string]bool)
	
	// Email patterns by type
	patterns := map[string][]string{
		"university": {"facilities@", "security@", "custodian@", "info@", "accessibility@"},
		"school":     {"facilities@", "office@", "info@"},
		"hospital":   {"facilities@", "safety@", "info@", "patientservices@"},
		"organization": {"facilities@", "info@", "contact@"},
		"operator":   {"info@", "contact@", "support@"},
		"city":       {"311@", "publicworks@", "parks@", "info@"},
		"building":   {"facilities@", "management@", "info@"},
	}
	
	for _, level := range hierarchy {
		if level.Domain == "" {
			continue
		}
		
		// Get patterns for this type
		typePatterns, ok := patterns[level.Type]
		if !ok {
			typePatterns = patterns["organization"] // default
		}
		
		for _, pattern := range typePatterns {
			email := pattern + level.Domain
			lower := strings.ToLower(email)
			if !seen[lower] && ValidateEmail(email) {
				emails = append(emails, email)
				seen[lower] = true
			}
		}
		
		// Add the direct contact email if available
		if level.ContactEmail != "" && !seen[strings.ToLower(level.ContactEmail)] {
			if ValidateEmail(level.ContactEmail) {
				emails = append(emails, level.ContactEmail)
				seen[strings.ToLower(level.ContactEmail)] = true
			}
		}
	}
	
	return emails
}
