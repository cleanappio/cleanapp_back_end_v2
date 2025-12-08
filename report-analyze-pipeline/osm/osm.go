package osm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// NominatimBaseURL is the public Nominatim API endpoint
	NominatimBaseURL = "https://nominatim.openstreetmap.org"
	// OverpassBaseURL is the public Overpass API endpoint
	OverpassBaseURL = "https://overpass-api.de/api/interpreter"
	// UserAgent is required by Nominatim usage policy
	UserAgent = "CleanApp/1.0 (https://cleanapp.io)"
	// Rate limit: 1 request per second for Nominatim
	minRequestInterval = time.Second
)

// Client handles OSM API interactions with rate limiting
type Client struct {
	httpClient    *http.Client
	lastRequest   time.Time
	rateLimitLock sync.Mutex
}

// NewClient creates a new OSM client with rate limiting
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// LocationContext contains extracted location information from OSM
type LocationContext struct {
	// Primary name of the location (building, POI, campus)
	PrimaryName string `json:"primary_name"`
	// Parent organization (e.g., university containing a department)
	ParentOrg string `json:"parent_org"`
	// Operator of the location
	Operator string `json:"operator"`
	// Inferred domain from website or org name
	Domain string `json:"domain"`
	// Contact email if present in OSM tags
	ContactEmail string `json:"contact_email"`
	// Address components
	Address Address `json:"address"`
	// Type of location (university, school, hospital, park, business, etc.)
	LocationType string `json:"location_type"`
	// Raw OSM tags for additional context
	Tags map[string]string `json:"tags"`
}

// Address contains parsed address components
type Address struct {
	HouseNumber string `json:"house_number"`
	Road        string `json:"road"`
	Suburb      string `json:"suburb"`
	City        string `json:"city"`
	County      string `json:"county"`
	State       string `json:"state"`
	PostCode    string `json:"postcode"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
}

// NominatimResponse is the response from Nominatim reverse geocoding
type NominatimResponse struct {
	PlaceID     int               `json:"place_id"`
	Licence     string            `json:"licence"`
	OSMType     string            `json:"osm_type"`
	OSMID       int               `json:"osm_id"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	DisplayName string            `json:"display_name"`
	Address     NominatimAddress  `json:"address"`
	Extratags   map[string]string `json:"extratags"`
	Namedetails map[string]string `json:"namedetails"`
	Type        string            `json:"type"`
	Class       string            `json:"class"`
	Name        string            `json:"name"`
}

// NominatimAddress contains address details from Nominatim
type NominatimAddress struct {
	Amenity       string `json:"amenity"`
	Building      string `json:"building"`
	HouseNumber   string `json:"house_number"`
	Road          string `json:"road"`
	Suburb        string `json:"suburb"`
	City          string `json:"city"`
	Town          string `json:"town"`
	Village       string `json:"village"`
	County        string `json:"county"`
	State         string `json:"state"`
	PostCode      string `json:"postcode"`
	Country       string `json:"country"`
	CountryCode   string `json:"country_code"`
	University    string `json:"university"`
	School        string `json:"school"`
	Hospital      string `json:"hospital"`
	ShoppingCentre string `json:"shopping_centre"`
	Mall          string `json:"mall"`
}

// enforceRateLimit ensures we don't exceed Nominatim's rate limit
func (c *Client) enforceRateLimit() {
	c.rateLimitLock.Lock()
	defer c.rateLimitLock.Unlock()

	elapsed := time.Since(c.lastRequest)
	if elapsed < minRequestInterval {
		time.Sleep(minRequestInterval - elapsed)
	}
	c.lastRequest = time.Now()
}

// ReverseGeocode performs reverse geocoding to get location context
func (c *Client) ReverseGeocode(lat, lon float64) (*LocationContext, error) {
	c.enforceRateLimit()

	// Build URL with required parameters
	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%f", lat))
	params.Set("lon", fmt.Sprintf("%f", lon))
	params.Set("format", "jsonv2")
	params.Set("addressdetails", "1")
	params.Set("extratags", "1")
	params.Set("namedetails", "1")
	params.Set("zoom", "18") // Building-level detail

	reqURL := fmt.Sprintf("%s/reverse?%s", NominatimBaseURL, params.Encode())

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nominatim returned status %d: %s", resp.StatusCode, string(body))
	}

	var nomResp NominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&nomResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseNominatimResponse(&nomResp), nil
}

// parseNominatimResponse extracts LocationContext from Nominatim response
func (c *Client) parseNominatimResponse(resp *NominatimResponse) *LocationContext {
	ctx := &LocationContext{
		Tags: make(map[string]string),
	}

	// Determine primary name
	if resp.Name != "" {
		ctx.PrimaryName = resp.Name
	} else if resp.Namedetails != nil {
		if name, ok := resp.Namedetails["name"]; ok {
			ctx.PrimaryName = name
		}
	}

	// Extract organization hierarchy
	if resp.Address.University != "" {
		ctx.ParentOrg = resp.Address.University
		ctx.LocationType = "university"
	} else if resp.Address.School != "" {
		ctx.ParentOrg = resp.Address.School
		ctx.LocationType = "school"
	} else if resp.Address.Hospital != "" {
		ctx.ParentOrg = resp.Address.Hospital
		ctx.LocationType = "hospital"
	} else if resp.Address.ShoppingCentre != "" || resp.Address.Mall != "" {
		if resp.Address.ShoppingCentre != "" {
			ctx.ParentOrg = resp.Address.ShoppingCentre
		} else {
			ctx.ParentOrg = resp.Address.Mall
		}
		ctx.LocationType = "shopping_center"
	} else if resp.Class == "amenity" {
		ctx.LocationType = resp.Type
	}

	// Extract operator and contact info from extratags
	if resp.Extratags != nil {
		if op, ok := resp.Extratags["operator"]; ok {
			ctx.Operator = op
		}
		if email, ok := resp.Extratags["contact:email"]; ok {
			ctx.ContactEmail = email
		} else if email, ok := resp.Extratags["email"]; ok {
			ctx.ContactEmail = email
		}
		// Try to extract domain from website
		if website, ok := resp.Extratags["website"]; ok {
			ctx.Domain = extractDomainFromURL(website)
		} else if website, ok := resp.Extratags["contact:website"]; ok {
			ctx.Domain = extractDomainFromURL(website)
		}
		// Store all extratags for context
		for k, v := range resp.Extratags {
			ctx.Tags[k] = v
		}
	}

	// Build address
	ctx.Address = Address{
		HouseNumber: resp.Address.HouseNumber,
		Road:        resp.Address.Road,
		Suburb:      resp.Address.Suburb,
		City:        firstNonEmpty(resp.Address.City, resp.Address.Town, resp.Address.Village),
		County:      resp.Address.County,
		State:       resp.Address.State,
		PostCode:    resp.Address.PostCode,
		Country:     resp.Address.Country,
		CountryCode: resp.Address.CountryCode,
	}

	return ctx
}

// extractDomainFromURL extracts domain from a URL string
func extractDomainFromURL(urlStr string) string {
	// Handle URLs without scheme
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	host := parsed.Hostname()
	// Remove www. prefix if present
	host = strings.TrimPrefix(host, "www.")
	return host
}

// firstNonEmpty returns the first non-empty string from the arguments
func firstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

// HasUsefulData returns true if the location context has enough data for email inference
func (ctx *LocationContext) HasUsefulData() bool {
	return ctx.PrimaryName != "" || ctx.ParentOrg != "" || ctx.Operator != "" || ctx.ContactEmail != ""
}

// GetOrganizationName returns the most specific organization name available
func (ctx *LocationContext) GetOrganizationName() string {
	if ctx.PrimaryName != "" && ctx.ParentOrg != "" && ctx.PrimaryName != ctx.ParentOrg {
		return fmt.Sprintf("%s, %s", ctx.PrimaryName, ctx.ParentOrg)
	}
	if ctx.PrimaryName != "" {
		return ctx.PrimaryName
	}
	if ctx.ParentOrg != "" {
		return ctx.ParentOrg
	}
	if ctx.Operator != "" {
		return ctx.Operator
	}
	return ""
}
