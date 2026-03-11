package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"report-listener/config"
	"report-listener/models"

	xhtml "golang.org/x/net/html"
)

const (
	caseDiscoveryUserAgent       = "CleanApp-Case-Discovery/1.0 (https://cleanapp.io)"
	caseDiscoveryNominatimBase   = "https://nominatim.openstreetmap.org"
	caseDiscoveryOverpassBase    = "https://overpass-api.de/api/interpreter"
	caseDiscoveryDuckDuckGoBase  = "https://html.duckduckgo.com/html/"
	caseDiscoveryMaxWebsiteFetch = 3
)

var (
	caseDiscoveryMailtoRegex        = regexp.MustCompile(`(?i)mailto:([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})`)
	caseDiscoveryEmailRegex         = regexp.MustCompile(`(?i)\b([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})\b`)
	caseDiscoveryTelRegex           = regexp.MustCompile(`(?i)tel:([^"'\s<>]+)`)
	caseDiscoveryPhoneRegex         = regexp.MustCompile(`\+?[0-9][0-9()\-\.\s]{6,}[0-9]`)
	caseDiscoveryHrefRegex          = regexp.MustCompile(`(?i)href=["']([^"'#]+)["']`)
	caseDiscoverySearchBlockedHosts = map[string]struct{}{
		"duckduckgo.com":        {},
		"google.com":            {},
		"www.google.com":        {},
		"maps.google.com":       {},
		"facebook.com":          {},
		"www.facebook.com":      {},
		"instagram.com":         {},
		"www.instagram.com":     {},
		"x.com":                 {},
		"twitter.com":           {},
		"www.x.com":             {},
		"www.twitter.com":       {},
		"linkedin.com":          {},
		"www.linkedin.com":      {},
		"youtube.com":           {},
		"www.youtube.com":       {},
		"mapcarta.com":          {},
		"www.mapcarta.com":      {},
		"wikimapia.org":         {},
		"www.wikimapia.org":     {},
		"openstreetmap.org":     {},
		"www.openstreetmap.org": {},
	}
)

type caseContactDiscoverer struct {
	httpClient          *http.Client
	googlePlacesAPIKey  string
	googlePlacesBaseURL string
	webSearchBaseURL    string

	nominatimMu       sync.Mutex
	lastNominatimCall time.Time
}

type caseDiscoverySeed struct {
	Latitude  float64
	Longitude float64
	Names     []string
}

type caseLocationContext struct {
	PrimaryName   string
	ParentOrg     string
	Operator      string
	Website       string
	ContactEmail  string
	ContactPhone  string
	LocationType  string
	City          string
	State         string
	Country       string
	CountryCode   string
	FormattedName string
	Tags          map[string]string
}

type casePOI struct {
	Name         string
	Operator     string
	Website      string
	ContactEmail string
	ContactPhone string
	Tags         map[string]string
	Latitude     float64
	Longitude    float64
}

type caseSocialRef struct {
	Platform string
	Handle   string
	URL      string
}

type caseWebsiteContacts struct {
	Website      string
	ContactPages []string
	Emails       []string
	Phones       []string
	Socials      []caseSocialRef
}

type caseStakeholderSearchQuery struct {
	RoleType        string
	Query           string
	Region          string
	BaseConfidence  float64
	Rationale       string
	Organization    string
	RelationshipTag string
}

type caseWebSearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
}

type caseTargetMerger struct {
	limit   int
	targets map[string]models.CaseEscalationTarget
}

type nominatimReverseResponse struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Class       string            `json:"class"`
	Type        string            `json:"type"`
	Address     map[string]string `json:"address"`
	ExtraTags   map[string]string `json:"extratags"`
	NameDetails map[string]string `json:"namedetails"`
}

type overpassResponse struct {
	Elements []struct {
		Type   string  `json:"type"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		Center *struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"center"`
		Tags map[string]string `json:"tags"`
	} `json:"elements"`
}

type googlePlacesSearchRequest struct {
	TextQuery      string `json:"textQuery"`
	MaxResultCount int    `json:"maxResultCount,omitempty"`
	LocationBias   struct {
		Circle struct {
			Center struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"center"`
			Radius float64 `json:"radius"`
		} `json:"circle"`
	} `json:"locationBias"`
}

type googlePlacesSearchResponse struct {
	Places []struct {
		DisplayName struct {
			Text string `json:"text"`
		} `json:"displayName"`
		FormattedAddress    string `json:"formattedAddress"`
		WebsiteURI          string `json:"websiteUri"`
		NationalPhoneNumber string `json:"nationalPhoneNumber"`
		GoogleMapsURI       string `json:"googleMapsUri"`
	} `json:"places"`
}

func newCaseContactDiscoverer(cfg *config.Config) *caseContactDiscoverer {
	return &caseContactDiscoverer{
		httpClient:          &http.Client{Timeout: 8 * time.Second},
		googlePlacesAPIKey:  strings.TrimSpace(cfg.GooglePlacesAPIKey),
		googlePlacesBaseURL: strings.TrimRight(strings.TrimSpace(cfg.GooglePlacesBaseURL), "/"),
		webSearchBaseURL:    strings.TrimRight(caseDiscoveryDuckDuckGoBase, "/"),
	}
}

func (h *Handlers) suggestEscalationTargets(ctx context.Context, geometryJSON string, reports []models.ReportWithAnalysis, limit int) ([]models.CaseEscalationTarget, error) {
	storedTargets, err := h.db.SuggestEscalationTargetsByGeometry(ctx, geometryJSON, reportSeqs(reports), limit)
	if err != nil {
		return nil, err
	}
	if h.contactDiscoverer == nil {
		return storedTargets, nil
	}
	return h.contactDiscoverer.EnrichTargets(ctx, reports, storedTargets, limit), nil
}

func (d *caseContactDiscoverer) EnrichTargets(ctx context.Context, reports []models.ReportWithAnalysis, existing []models.CaseEscalationTarget, limit int) []models.CaseEscalationTarget {
	merger := newCaseTargetMerger(limit)
	inferredFallback := make([]models.CaseEscalationTarget, 0, len(existing))
	for _, target := range existing {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			inferredFallback = append(inferredFallback, target)
			continue
		}
		merger.Add(target)
	}
	if len(reports) == 0 {
		if countPreferredCaseTargets(merger.Targets()) == 0 {
			for _, target := range inferredFallback {
				merger.Add(target)
			}
		}
		return merger.Targets()
	}

	visitedWebsites := make(map[string]struct{})
	searchedQueries := make(map[string]struct{})
	structuralMode := shouldExpandStructuralStakeholderDiscovery(reports)
	for _, seed := range buildCaseDiscoverySeeds(reports, 2) {
		if ctx.Err() != nil {
			break
		}
		candidateNames := append([]string(nil), seed.Names...)

		locCtx, err := d.reverseGeocode(ctx, seed.Latitude, seed.Longitude)
		if err != nil {
			log.Printf("warn: case reverse geocode failed at %.5f,%.5f: %v", seed.Latitude, seed.Longitude, err)
		} else if locCtx != nil {
			candidateNames = appendUniqueStrings(candidateNames, locCtx.PrimaryName, locCtx.ParentOrg, locCtx.Operator)
			d.addLocationContextTargets(ctx, locCtx, merger, visitedWebsites)
		}

		pois, err := d.queryNearbyPOIs(ctx, seed.Latitude, seed.Longitude, 225)
		if err != nil {
			log.Printf("warn: case Overpass query failed at %.5f,%.5f: %v", seed.Latitude, seed.Longitude, err)
		} else {
			for _, poi := range rankCasePOIs(seed, candidateNames, pois) {
				d.addPOITargets(ctx, poi, merger, visitedWebsites)
				candidateNames = appendUniqueStrings(candidateNames, poi.Name, poi.Operator)
				if len(candidateNames) >= 6 {
					candidateNames = candidateNames[:6]
				}
			}
		}

		if d.googlePlacesAPIKey != "" {
			for _, name := range candidateNames {
				queryKey := strings.ToLower(strings.TrimSpace(name))
				if queryKey == "" {
					continue
				}
				if _, seen := searchedQueries[queryKey]; seen {
					continue
				}
				searchedQueries[queryKey] = struct{}{}
				places, err := d.searchGooglePlaces(ctx, name, seed.Latitude, seed.Longitude)
				if err != nil {
					log.Printf("warn: case Google Places search failed for %q: %v", name, err)
					continue
				}
				for _, place := range places {
					d.addGooglePlaceTargets(ctx, place, merger, visitedWebsites)
				}
				if len(searchedQueries) >= 3 {
					break
				}
			}
		}

		if d.webSearchBaseURL != "" {
			queries := buildCaseStakeholderSearchQueries(candidateNames, locCtx, structuralMode)
			d.addWebSearchStakeholderTargets(ctx, queries, merger, visitedWebsites, searchedQueries)
		}
	}

	if countPreferredCaseTargets(merger.Targets()) == 0 {
		for _, target := range inferredFallback {
			merger.Add(target)
		}
	}
	return merger.Targets()
}

func buildCaseDiscoverySeeds(reports []models.ReportWithAnalysis, maxSeeds int) []caseDiscoverySeed {
	if maxSeeds <= 0 {
		maxSeeds = 1
	}
	ordered := append([]models.ReportWithAnalysis(nil), reports...)
	sort.Slice(ordered, func(i, j int) bool {
		leftSeverity := preferredSeverity(&ordered[i])
		rightSeverity := preferredSeverity(&ordered[j])
		if leftSeverity == rightSeverity {
			return ordered[i].Report.Timestamp.After(ordered[j].Report.Timestamp)
		}
		return leftSeverity > rightSeverity
	})

	seen := make(map[string]struct{})
	seeds := make([]caseDiscoverySeed, 0, maxSeeds)
	for _, report := range ordered {
		key := fmt.Sprintf("%.4f:%.4f", report.Report.Latitude, report.Report.Longitude)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		analysis := preferredAnalysis(&report)
		names := []string{}
		if analysis != nil {
			names = appendUniqueStrings(names,
				strings.TrimSpace(analysis.BrandDisplayName),
				strings.TrimSpace(analysis.BrandName),
				strings.TrimSpace(analysis.Title),
			)
		}
		seeds = append(seeds, caseDiscoverySeed{
			Latitude:  report.Report.Latitude,
			Longitude: report.Report.Longitude,
			Names:     names,
		})
		if len(seeds) >= maxSeeds {
			break
		}
	}
	return seeds
}

func shouldExpandStructuralStakeholderDiscovery(reports []models.ReportWithAnalysis) bool {
	keywords := []string{
		"structural", "crack", "cracking", "brick", "bricks", "facade", "façade", "masonry",
		"wall", "concrete", "beam", "column", "roof", "ceiling", "collapse", "falling",
		"exterior", "foundation", "balcony", "spalling", "detachment", "separation",
		"fissure", "fassade", "fassaden", "mauerwerk", "beton", "riss", "risse",
	}
	for _, report := range reports {
		if preferredClassification(&report) != "physical" {
			continue
		}
		analysis := preferredAnalysis(&report)
		if analysis == nil {
			continue
		}
		text := strings.ToLower(strings.Join([]string{
			analysis.Title,
			analysis.Summary,
			analysis.Description,
		}, " "))
		matchCount := 0
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				matchCount++
				if matchCount >= 2 || analysis.SeverityLevel >= 0.8 {
					return true
				}
			}
		}
	}
	return false
}

func countPreferredCaseTargets(targets []models.CaseEscalationTarget) int {
	count := 0
	for _, target := range targets {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			continue
		}
		if hasCaseDiscoveryContactMethod(target) {
			count++
		}
	}
	return count
}

func buildCaseStakeholderSearchQueries(candidateNames []string, locCtx *caseLocationContext, structuralMode bool) []caseStakeholderSearchQuery {
	primaryName := firstNonEmpty(candidateNames...)
	if locCtx != nil {
		primaryName = firstNonEmpty(primaryName, locCtx.PrimaryName, locCtx.ParentOrg, locCtx.Operator)
	}
	primaryName = strings.TrimSpace(primaryName)
	if primaryName == "" {
		return nil
	}

	locality := ""
	region := ""
	if locCtx != nil {
		locality = firstNonEmpty(locCtx.City, locCtx.State, locCtx.Country)
		region = webSearchRegionForLocation(locCtx)
	}

	baseQuery := quotedSearchPhrase(primaryName)
	if locality != "" {
		baseQuery += " " + quotedSearchPhrase(locality)
	}

	queries := []caseStakeholderSearchQuery{
		{
			RoleType:        "operator",
			Query:           strings.TrimSpace(baseQuery + " contact"),
			Region:          region,
			BaseConfidence:  0.84,
			Rationale:       "Official site/contact search for the affected location.",
			Organization:    primaryName,
			RelationshipTag: "site operator",
		},
	}

	if !structuralMode {
		return queries
	}

	architectTerm := "architect"
	contractorTerm := "contractor"
	engineerTerm := "structural engineer"
	authorityTerm := "building department"
	if isGermanSpeakingLocation(locCtx) {
		architectTerm = "architekt"
		contractorTerm = "bauunternehmung"
		engineerTerm = "ingenieur"
		authorityTerm = "bauamt"
	}

	if locality != "" {
		queries = append(queries, caseStakeholderSearchQuery{
			RoleType:        "architect",
			Query:           strings.TrimSpace(baseQuery + " " + architectTerm),
			Region:          region,
			BaseConfidence:  0.83,
			Rationale:       "Web search for the architect or design office linked to the structurally affected site.",
			Organization:    primaryName,
			RelationshipTag: "architect",
		})
		queries = append(queries, caseStakeholderSearchQuery{
			RoleType:        "contractor",
			Query:           strings.TrimSpace(baseQuery + " " + contractorTerm),
			Region:          region,
			BaseConfidence:  0.81,
			Rationale:       "Web search for the contractor or builder linked to the structurally affected site.",
			Organization:    primaryName,
			RelationshipTag: "contractor",
		})
		queries = append(queries, caseStakeholderSearchQuery{
			RoleType:        "engineer",
			Query:           strings.TrimSpace(baseQuery + " " + engineerTerm),
			Region:          region,
			BaseConfidence:  0.79,
			Rationale:       "Web search for structural engineering stakeholders tied to the affected site.",
			Organization:    primaryName,
			RelationshipTag: "engineer",
		})
		queries = append(queries, caseStakeholderSearchQuery{
			RoleType:        "building_authority",
			Query:           strings.TrimSpace(quotedSearchPhrase(locality) + " " + authorityTerm + " " + quotedSearchPhrase(primaryName)),
			Region:          region,
			BaseConfidence:  0.77,
			Rationale:       "Web search for the local building authority or municipal office responsible for the affected site.",
			Organization:    locality,
			RelationshipTag: "authority",
		})
	}

	return queries
}

func webSearchRegionForLocation(locCtx *caseLocationContext) string {
	if locCtx == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(locCtx.CountryCode)) {
	case "ch":
		if isFrenchSpeakingSwissState(locCtx.State) {
			return "ch-fr"
		}
		if isItalianSpeakingSwissState(locCtx.State) {
			return "ch-it"
		}
		return "ch-de"
	case "de":
		return "de-de"
	case "at":
		return "at-de"
	case "fr":
		return "fr-fr"
	case "it":
		return "it-it"
	case "es":
		return "es-es"
	case "pt":
		return "pt-pt"
	default:
		return ""
	}
}

func isGermanSpeakingLocation(locCtx *caseLocationContext) bool {
	if locCtx == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(locCtx.CountryCode)) {
	case "de", "at", "li":
		return true
	case "ch":
		return !isFrenchSpeakingSwissState(locCtx.State) && !isItalianSpeakingSwissState(locCtx.State)
	default:
		return strings.Contains(strings.ToLower(locCtx.State), "zürich") ||
			strings.Contains(strings.ToLower(locCtx.State), "zurich")
	}
}

func isFrenchSpeakingSwissState(state string) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	return strings.Contains(state, "genève") ||
		strings.Contains(state, "geneva") ||
		strings.Contains(state, "vaud") ||
		strings.Contains(state, "neuchâtel") ||
		strings.Contains(state, "neuchatel") ||
		strings.Contains(state, "jura") ||
		strings.Contains(state, "fribourg")
}

func isItalianSpeakingSwissState(state string) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	return strings.Contains(state, "ticino")
}

func quotedSearchPhrase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, "\"") {
		value = strings.ReplaceAll(value, "\"", "")
	}
	return `"` + value + `"`
}

func (d *caseContactDiscoverer) reverseGeocode(ctx context.Context, latitude, longitude float64) (*caseLocationContext, error) {
	if err := d.enforceNominatimRateLimit(ctx); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%.6f", latitude))
	params.Set("lon", fmt.Sprintf("%.6f", longitude))
	params.Set("format", "jsonv2")
	params.Set("addressdetails", "1")
	params.Set("extratags", "1")
	params.Set("namedetails", "1")
	params.Set("zoom", "18")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, caseDiscoveryNominatimBase+"/reverse?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", caseDiscoveryUserAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("nominatim returned http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded nominatimReverseResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	website := firstNonEmpty(decoded.ExtraTags["website"], decoded.ExtraTags["contact:website"], decoded.ExtraTags["operator:website"])
	ctxOut := &caseLocationContext{
		PrimaryName:   firstNonEmpty(decoded.Name, decoded.NameDetails["name"], decoded.Address["amenity"], decoded.Address["building"]),
		ParentOrg:     firstNonEmpty(decoded.Address["university"], decoded.Address["school"], decoded.Address["hospital"], decoded.Address["college"]),
		Operator:      firstNonEmpty(decoded.ExtraTags["operator"], decoded.Address["office"]),
		Website:       normalizeWebsiteURL(website),
		ContactEmail:  normalizeEmail(firstNonEmpty(decoded.ExtraTags["contact:email"], decoded.ExtraTags["email"])),
		ContactPhone:  normalizePhone(firstNonEmpty(decoded.ExtraTags["contact:phone"], decoded.ExtraTags["phone"])),
		LocationType:  firstNonEmpty(decoded.Type, decoded.Class),
		City:          firstNonEmpty(decoded.Address["city"], decoded.Address["town"], decoded.Address["village"]),
		State:         decoded.Address["state"],
		Country:       decoded.Address["country"],
		CountryCode:   decoded.Address["country_code"],
		FormattedName: decoded.DisplayName,
		Tags:          decoded.ExtraTags,
	}
	if ctxOut.PrimaryName == "" && ctxOut.ParentOrg == "" && ctxOut.Operator == "" && ctxOut.Website == "" && ctxOut.ContactEmail == "" && ctxOut.ContactPhone == "" {
		return nil, nil
	}
	return ctxOut, nil
}

func (d *caseContactDiscoverer) queryNearbyPOIs(ctx context.Context, latitude, longitude float64, radiusMeters int) ([]casePOI, error) {
	query := fmt.Sprintf(`
[out:json][timeout:20];
(
  nwr["amenity"~"university|college|school|hospital|clinic|library|community_centre|townhall|courthouse"](around:%d,%.6f,%.6f);
  nwr["office"](around:%d,%.6f,%.6f);
  nwr["building"]["name"](around:%d,%.6f,%.6f);
  nwr["shop"](around:%d,%.6f,%.6f);
  nwr["tourism"~"hotel|museum|attraction"](around:%d,%.6f,%.6f);
);
out center;`,
		radiusMeters, latitude, longitude,
		radiusMeters, latitude, longitude,
		radiusMeters, latitude, longitude,
		radiusMeters, latitude, longitude,
		radiusMeters, latitude, longitude,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, caseDiscoveryOverpassBase+"?data="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", caseDiscoveryUserAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("overpass returned http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded overpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	pois := make([]casePOI, 0, len(decoded.Elements))
	for _, elem := range decoded.Elements {
		lat := elem.Lat
		lon := elem.Lon
		if elem.Center != nil {
			lat = elem.Center.Lat
			lon = elem.Center.Lon
		}
		website := normalizeWebsiteURL(firstNonEmpty(elem.Tags["website"], elem.Tags["contact:website"], elem.Tags["operator:website"]))
		poi := casePOI{
			Name:         strings.TrimSpace(elem.Tags["name"]),
			Operator:     strings.TrimSpace(elem.Tags["operator"]),
			Website:      website,
			ContactEmail: normalizeEmail(firstNonEmpty(elem.Tags["contact:email"], elem.Tags["email"])),
			ContactPhone: normalizePhone(firstNonEmpty(elem.Tags["contact:phone"], elem.Tags["phone"])),
			Tags:         elem.Tags,
			Latitude:     lat,
			Longitude:    lon,
		}
		if poi.Name == "" && poi.Operator == "" && poi.Website == "" && poi.ContactEmail == "" && poi.ContactPhone == "" {
			continue
		}
		pois = append(pois, poi)
	}
	return pois, nil
}

func rankCasePOIs(seed caseDiscoverySeed, names []string, pois []casePOI) []casePOI {
	if len(pois) <= 1 {
		return pois
	}
	ordered := append([]casePOI(nil), pois...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := casePOIScore(seed, names, ordered[i])
		right := casePOIScore(seed, names, ordered[j])
		if left == right {
			return ordered[i].Name < ordered[j].Name
		}
		return left > right
	})
	if len(ordered) > 4 {
		ordered = ordered[:4]
	}
	return ordered
}

func casePOIScore(seed caseDiscoverySeed, names []string, poi casePOI) float64 {
	score := 0.0
	if poi.ContactEmail != "" {
		score += 4
	}
	if poi.ContactPhone != "" {
		score += 3
	}
	if poi.Website != "" {
		score += 2
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		lowerName := strings.ToLower(name)
		if strings.Contains(strings.ToLower(poi.Name), lowerName) || strings.Contains(strings.ToLower(poi.Operator), lowerName) {
			score += 3
		}
	}
	if poi.Latitude != 0 || poi.Longitude != 0 {
		score -= haversineMeters(seed.Latitude, seed.Longitude, poi.Latitude, poi.Longitude) / 200.0
	}
	return score
}

func (d *caseContactDiscoverer) addLocationContextTargets(ctx context.Context, locCtx *caseLocationContext, merger *caseTargetMerger, visitedWebsites map[string]struct{}) {
	organization := firstNonEmpty(locCtx.PrimaryName, locCtx.ParentOrg, locCtx.Operator)
	roleType := "operator"
	if locCtx.ContactEmail != "" {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "email",
			Email:           locCtx.ContactEmail,
			TargetSource:    "osm_reverse",
			ConfidenceScore: 0.92,
			Rationale:       fmt.Sprintf("Direct contact email published in OpenStreetMap tags for %s.", organization),
		})
	}
	if locCtx.ContactPhone != "" {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "phone",
			Phone:           locCtx.ContactPhone,
			TargetSource:    "osm_reverse",
			ConfidenceScore: 0.9,
			Rationale:       fmt.Sprintf("Direct phone number published in OpenStreetMap tags for %s.", organization),
		})
	}
	for _, social := range socialRefsFromTags(locCtx.Tags) {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "social",
			ContactURL:      social.URL,
			SocialPlatform:  social.Platform,
			SocialHandle:    social.Handle,
			TargetSource:    "osm_reverse",
			ConfidenceScore: 0.78,
			Rationale:       fmt.Sprintf("Social profile published in OpenStreetMap tags for %s.", organization),
		})
	}
	if locCtx.Website != "" {
		d.addWebsiteTargets(ctx, roleType, organization, locCtx.Website, locCtx.Website, "osm_reverse", 0.8, "Official website discovered from OpenStreetMap tags.", merger, visitedWebsites)
	}
}

func (d *caseContactDiscoverer) addPOITargets(ctx context.Context, poi casePOI, merger *caseTargetMerger, visitedWebsites map[string]struct{}) {
	organization := firstNonEmpty(poi.Name, poi.Operator)
	if organization == "" {
		organization = "Nearby location"
	}
	roleType := "operator"
	if poi.ContactEmail != "" {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "email",
			Email:           poi.ContactEmail,
			TargetSource:    "osm_poi",
			ConfidenceScore: 0.88,
			Rationale:       fmt.Sprintf("Direct email published by nearby OSM POI %s.", organization),
		})
	}
	if poi.ContactPhone != "" {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "phone",
			Phone:           poi.ContactPhone,
			TargetSource:    "osm_poi",
			ConfidenceScore: 0.86,
			Rationale:       fmt.Sprintf("Direct phone number published by nearby OSM POI %s.", organization),
		})
	}
	for _, social := range socialRefsFromTags(poi.Tags) {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "social",
			ContactURL:      social.URL,
			SocialPlatform:  social.Platform,
			SocialHandle:    social.Handle,
			TargetSource:    "osm_poi",
			ConfidenceScore: 0.74,
			Rationale:       fmt.Sprintf("Social profile published by nearby OSM POI %s.", organization),
		})
	}
	if poi.Website != "" {
		d.addWebsiteTargets(ctx, roleType, organization, poi.Website, poi.Website, "osm_poi", 0.76, "Official website discovered from a nearby OSM place.", merger, visitedWebsites)
	}
}

func (d *caseContactDiscoverer) searchGooglePlaces(ctx context.Context, name string, latitude, longitude float64) ([]googlePlacesSearchResponsePlace, error) {
	requestBody := googlePlacesSearchRequest{TextQuery: name, MaxResultCount: 3}
	requestBody.LocationBias.Circle.Center.Latitude = latitude
	requestBody.LocationBias.Circle.Center.Longitude = longitude
	requestBody.LocationBias.Circle.Radius = 250

	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.googlePlacesBaseURL+"/places:searchText", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", d.googlePlacesAPIKey)
	req.Header.Set("X-Goog-FieldMask", "places.displayName,places.formattedAddress,places.websiteUri,places.nationalPhoneNumber,places.googleMapsUri")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("google places returned http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded googlePlacesSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	places := make([]googlePlacesSearchResponsePlace, 0, len(decoded.Places))
	for _, place := range decoded.Places {
		places = append(places, googlePlacesSearchResponsePlace{
			DisplayName:         strings.TrimSpace(place.DisplayName.Text),
			FormattedAddress:    strings.TrimSpace(place.FormattedAddress),
			WebsiteURI:          normalizeWebsiteURL(place.WebsiteURI),
			NationalPhoneNumber: normalizePhone(place.NationalPhoneNumber),
			GoogleMapsURI:       strings.TrimSpace(place.GoogleMapsURI),
		})
	}
	return places, nil
}

type googlePlacesSearchResponsePlace struct {
	DisplayName         string
	FormattedAddress    string
	WebsiteURI          string
	NationalPhoneNumber string
	GoogleMapsURI       string
}

func (d *caseContactDiscoverer) addGooglePlaceTargets(ctx context.Context, place googlePlacesSearchResponsePlace, merger *caseTargetMerger, visitedWebsites map[string]struct{}) {
	organization := firstNonEmpty(place.DisplayName, place.FormattedAddress)
	if organization == "" {
		organization = "Google Places result"
	}
	roleType := "operator"
	if place.NationalPhoneNumber != "" {
		merger.Add(models.CaseEscalationTarget{
			RoleType:        roleType,
			Organization:    organization,
			DisplayName:     organization,
			Channel:         "phone",
			Phone:           place.NationalPhoneNumber,
			TargetSource:    "google_places",
			ConfidenceScore: 0.84,
			Rationale:       fmt.Sprintf("Phone number returned by Google Places for %s.", organization),
		})
	}
	if place.WebsiteURI != "" {
		d.addWebsiteTargets(ctx, roleType, organization, place.WebsiteURI, place.GoogleMapsURI, "google_places", 0.79, "Official website returned by Google Places.", merger, visitedWebsites)
	}
}

func (d *caseContactDiscoverer) addWebSearchStakeholderTargets(ctx context.Context, queries []caseStakeholderSearchQuery, merger *caseTargetMerger, visitedWebsites, searchedQueries map[string]struct{}) {
	for _, query := range queries {
		if len(searchedQueries) >= 4 {
			return
		}
		queryKey := strings.ToLower(strings.TrimSpace(query.RoleType + ":" + query.Query))
		if queryKey == "" {
			continue
		}
		if _, exists := searchedQueries[queryKey]; exists {
			continue
		}
		searchedQueries[queryKey] = struct{}{}

		results, err := d.searchStakeholderWeb(ctx, query.Query, query.Region)
		if err != nil {
			log.Printf("warn: stakeholder web search failed for %q: %v", query.Query, err)
			continue
		}

		added := 0
		for _, result := range results {
			if isBlockedStakeholderSearchResult(result.URL) {
				continue
			}
			organization := firstNonEmpty(inferStakeholderOrganization(result, query), query.Organization)
			if organization == "" {
				continue
			}
			rationale := query.Rationale
			if snippet := strings.TrimSpace(result.Snippet); snippet != "" {
				rationale = strings.TrimSpace(rationale + " " + snippet)
			}
			d.addWebsiteTargets(
				ctx,
				query.RoleType,
				organization,
				result.URL,
				result.URL,
				"web_search",
				query.BaseConfidence,
				rationale,
				merger,
				visitedWebsites,
			)
			added++
			if added >= 2 {
				break
			}
		}
	}
}

func (d *caseContactDiscoverer) searchStakeholderWeb(ctx context.Context, query, region string) ([]caseWebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	searchURL := d.webSearchBaseURL
	if searchURL == "" {
		searchURL = strings.TrimRight(caseDiscoveryDuckDuckGoBase, "/")
	}

	params := url.Values{}
	params.Set("q", query)
	if strings.TrimSpace(region) != "" {
		params.Set("kl", region)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", caseDiscoveryUserAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("stakeholder search returned http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGoSearchResults(string(body)), nil
}

func parseDuckDuckGoSearchResults(raw string) []caseWebSearchResult {
	doc, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}

	results := make([]caseWebSearchResult, 0, 6)
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "div" && hasClassToken(node, "result") && hasClassToken(node, "results_links") {
			if result, ok := extractDuckDuckGoSearchResult(node); ok {
				results = append(results, result)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if len(results) > 6 {
		return results[:6]
	}
	return results
}

func extractDuckDuckGoSearchResult(node *xhtml.Node) (caseWebSearchResult, bool) {
	titleNode := findDescendantByClass(node, "result__a")
	if titleNode == nil {
		return caseWebSearchResult{}, false
	}
	rawHref := strings.TrimSpace(attrValue(titleNode, "href"))
	resolvedURL := unwrapDuckDuckGoResultURL(rawHref)
	if resolvedURL == "" {
		return caseWebSearchResult{}, false
	}
	result := caseWebSearchResult{
		Title:      strings.TrimSpace(nodeText(titleNode)),
		URL:        resolvedURL,
		DisplayURL: strings.TrimSpace(nodeText(findDescendantByClass(node, "result__url"))),
		Snippet:    strings.TrimSpace(nodeText(findDescendantByClass(node, "result__snippet"))),
	}
	if result.Title == "" {
		return caseWebSearchResult{}, false
	}
	result.Title = stdhtml.UnescapeString(result.Title)
	result.DisplayURL = stdhtml.UnescapeString(result.DisplayURL)
	result.Snippet = stdhtml.UnescapeString(result.Snippet)
	return result, true
}

func unwrapDuckDuckGoResultURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return normalizeWebsiteURL(raw)
	}
	if strings.Contains(parsed.Hostname(), "duckduckgo.com") {
		if uddg := strings.TrimSpace(parsed.Query().Get("uddg")); uddg != "" {
			if decoded, err := url.QueryUnescape(uddg); err == nil {
				return normalizeWebsiteURL(decoded)
			}
			return normalizeWebsiteURL(uddg)
		}
	}
	return normalizeWebsiteURL(raw)
}

func inferStakeholderOrganization(result caseWebSearchResult, query caseStakeholderSearchQuery) string {
	for _, candidate := range splitSearchTitleCandidates(result.Title) {
		normalized := strings.TrimSpace(candidate)
		if normalized == "" {
			continue
		}
		lower := strings.ToLower(normalized)
		if strings.Contains(lower, strings.ToLower(query.Organization)) && len(strings.Fields(normalized)) <= 6 {
			continue
		}
		if strings.Contains(lower, "referenz") || strings.Contains(lower, "reference") {
			continue
		}
		return normalized
	}
	if hostname := organizationFromHost(result.URL); hostname != "" {
		return hostname
	}
	return strings.TrimSpace(query.Organization)
}

func splitSearchTitleCandidates(title string) []string {
	replacer := strings.NewReplacer("—", "|", "–", "|", "-", "|", "·", "|", ":", "|")
	parts := strings.Split(replacer.Replace(title), "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func isBlockedStakeholderSearchResult(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return true
	}
	if _, blocked := caseDiscoverySearchBlockedHosts[host]; blocked {
		return true
	}
	lowerPath := strings.ToLower(parsed.Path)
	return strings.HasSuffix(lowerPath, ".pdf")
}

func organizationFromHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}
	name := parts[0]
	if len(parts) > 2 {
		name = parts[len(parts)-2]
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return titleizeWords(name)
}

func titleizeWords(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func (d *caseContactDiscoverer) addWebsiteTargets(ctx context.Context, roleType, organization, websiteURL, fallbackContactURL, source string, baseConfidence float64, rationale string, merger *caseTargetMerger, visitedWebsites map[string]struct{}) {
	normalizedWebsite := normalizeWebsiteURL(websiteURL)
	if normalizedWebsite == "" {
		return
	}
	websiteKey := canonicalURLKey(normalizedWebsite)
	if websiteKey == "" {
		websiteKey = normalizedWebsite
	}
	contactURL := strings.TrimSpace(fallbackContactURL)
	if _, seen := visitedWebsites[websiteKey]; !seen {
		visitedWebsites[websiteKey] = struct{}{}
		contacts, err := d.scrapeWebsiteContacts(ctx, normalizedWebsite)
		if err != nil {
			log.Printf("warn: website scrape failed for %s: %v", normalizedWebsite, err)
		} else {
			if contacts.Website != "" {
				normalizedWebsite = contacts.Website
			}
			if len(contacts.ContactPages) > 0 {
				contactURL = contacts.ContactPages[0]
			}
			for _, email := range contacts.Emails {
				merger.Add(models.CaseEscalationTarget{
					RoleType:        roleType,
					Organization:    organization,
					DisplayName:     organization,
					Channel:         "email",
					Email:           email,
					Website:         normalizedWebsite,
					ContactURL:      contactURL,
					TargetSource:    source + "_website",
					ConfidenceScore: baseConfidence,
					Rationale:       fmt.Sprintf("Email scraped from the official website for %s.", organization),
				})
			}
			for _, phone := range contacts.Phones {
				merger.Add(models.CaseEscalationTarget{
					RoleType:        roleType,
					Organization:    organization,
					DisplayName:     organization,
					Channel:         "phone",
					Phone:           phone,
					Website:         normalizedWebsite,
					ContactURL:      contactURL,
					TargetSource:    source + "_website",
					ConfidenceScore: baseConfidence - 0.02,
					Rationale:       fmt.Sprintf("Phone number scraped from the official website for %s.", organization),
				})
			}
			for _, social := range contacts.Socials {
				merger.Add(models.CaseEscalationTarget{
					RoleType:        roleType,
					Organization:    organization,
					DisplayName:     organization,
					Channel:         "social",
					Website:         normalizedWebsite,
					ContactURL:      social.URL,
					SocialPlatform:  social.Platform,
					SocialHandle:    social.Handle,
					TargetSource:    source + "_website",
					ConfidenceScore: baseConfidence - 0.05,
					Rationale:       fmt.Sprintf("Social profile linked from the official website for %s.", organization),
				})
			}
		}
	}
	merger.Add(models.CaseEscalationTarget{
		RoleType:        roleType,
		Organization:    organization,
		DisplayName:     organization,
		Channel:         "website",
		Website:         normalizedWebsite,
		ContactURL:      firstNonEmpty(contactURL, normalizedWebsite),
		TargetSource:    source,
		ConfidenceScore: baseConfidence - 0.08,
		Rationale:       rationale,
	})
}

func (d *caseContactDiscoverer) scrapeWebsiteContacts(ctx context.Context, rawWebsiteURL string) (*caseWebsiteContacts, error) {
	websiteURL := normalizeWebsiteURL(rawWebsiteURL)
	if websiteURL == "" {
		return nil, nil
	}
	parsedBase, err := url.Parse(websiteURL)
	if err != nil {
		return nil, err
	}

	result := &caseWebsiteContacts{Website: websiteURL}
	fetched := make(map[string]struct{})
	queue := []string{websiteURL}
	for _, candidate := range []string{"/contact", "/contact-us", "/about", "/impressum"} {
		queue = append(queue, parsedBase.ResolveReference(&url.URL{Path: candidate}).String())
	}

	for i := 0; i < len(queue) && len(fetched) < caseDiscoveryMaxWebsiteFetch; i++ {
		candidate := normalizeWebsiteURL(queue[i])
		if candidate == "" {
			continue
		}
		key := canonicalURLKey(candidate)
		if _, seen := fetched[key]; seen {
			continue
		}
		fetched[key] = struct{}{}
		finalURL, htmlBody, err := d.fetchHTML(ctx, candidate)
		if err != nil {
			continue
		}
		if result.Website == "" {
			result.Website = finalURL
		}
		result.Emails = appendUniqueStrings(result.Emails, extractEmailsFromHTML(htmlBody)...)
		result.Phones = appendUniqueStrings(result.Phones, extractPhonesFromHTML(htmlBody)...)
		for _, social := range extractSocialRefsFromHTML(htmlBody) {
			result.Socials = appendUniqueSocials(result.Socials, social)
		}
		finalParsed, err := url.Parse(finalURL)
		if err == nil {
			for _, link := range extractContactLinks(finalParsed, htmlBody) {
				queue = append(queue, link)
				result.ContactPages = appendUniqueStrings(result.ContactPages, link)
			}
		}
	}
	return result, nil
}

func (d *caseContactDiscoverer) fetchHTML(ctx context.Context, rawURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", caseDiscoveryUserAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("website returned http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", "", err
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return finalURL, string(body), nil
}

func (d *caseContactDiscoverer) enforceNominatimRateLimit(ctx context.Context) error {
	d.nominatimMu.Lock()
	defer d.nominatimMu.Unlock()
	wait := time.Until(d.lastNominatimCall.Add(time.Second))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	d.lastNominatimCall = time.Now()
	return nil
}

func newCaseTargetMerger(limit int) *caseTargetMerger {
	if limit <= 0 {
		limit = 8
	}
	return &caseTargetMerger{limit: limit, targets: make(map[string]models.CaseEscalationTarget)}
}

func (m *caseTargetMerger) Add(target models.CaseEscalationTarget) {
	normalized, ok := normalizeCaseEscalationTarget(target)
	if !ok {
		return
	}
	key := caseEscalationTargetKey(normalized)
	if key == "" {
		return
	}
	if existing, found := m.targets[key]; found {
		m.targets[key] = mergeCaseEscalationTargets(existing, normalized)
		return
	}
	m.targets[key] = normalized
}

func (m *caseTargetMerger) Targets() []models.CaseEscalationTarget {
	out := make([]models.CaseEscalationTarget, 0, len(m.targets))
	for _, target := range m.targets {
		out = append(out, target)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ConfidenceScore == out[j].ConfidenceScore {
			if caseChannelRank(out[i].Channel) == caseChannelRank(out[j].Channel) {
				if out[i].Organization == out[j].Organization {
					return caseEscalationTargetKey(out[i]) < caseEscalationTargetKey(out[j])
				}
				return out[i].Organization < out[j].Organization
			}
			return caseChannelRank(out[i].Channel) < caseChannelRank(out[j].Channel)
		}
		return out[i].ConfidenceScore > out[j].ConfidenceScore
	})
	if len(out) > m.limit {
		out = out[:m.limit]
	}
	return out
}

func hasCaseDiscoveryContactMethod(target models.CaseEscalationTarget) bool {
	return strings.TrimSpace(target.Email) != "" ||
		strings.TrimSpace(target.Phone) != "" ||
		strings.TrimSpace(target.Website) != "" ||
		strings.TrimSpace(target.ContactURL) != "" ||
		strings.TrimSpace(target.SocialHandle) != ""
}

func caseDiscoveryTargetChannel(target models.CaseEscalationTarget) string {
	if channel := strings.TrimSpace(target.Channel); channel != "" {
		return channel
	}
	if strings.TrimSpace(target.Email) != "" {
		return "email"
	}
	if strings.TrimSpace(target.Phone) != "" {
		return "phone"
	}
	if strings.TrimSpace(target.SocialHandle) != "" {
		return "social"
	}
	if strings.TrimSpace(target.Website) != "" || strings.TrimSpace(target.ContactURL) != "" {
		return "website"
	}
	return ""
}

func normalizeCaseEscalationTarget(target models.CaseEscalationTarget) (models.CaseEscalationTarget, bool) {
	target.RoleType = emptyDefault(strings.TrimSpace(target.RoleType), "contact")
	target.Organization = strings.TrimSpace(target.Organization)
	target.DisplayName = strings.TrimSpace(target.DisplayName)
	target.Email = normalizeEmail(target.Email)
	target.Phone = normalizePhone(target.Phone)
	target.Website = normalizeWebsiteURL(target.Website)
	target.ContactURL = normalizeFlexibleURL(target.ContactURL)
	target.SocialPlatform = normalizeSocialPlatform(target.SocialPlatform)
	target.SocialHandle = normalizeSocialHandle(target.SocialHandle)
	target.TargetSource = emptyDefault(strings.TrimSpace(target.TargetSource), "suggested")
	target.Rationale = strings.TrimSpace(target.Rationale)
	if target.DisplayName == "" {
		target.DisplayName = target.Organization
	}
	if target.Channel == "" {
		target.Channel = caseDiscoveryTargetChannel(target)
	} else {
		target.Channel = strings.TrimSpace(target.Channel)
	}
	if target.Channel == "social" && target.SocialPlatform == "" {
		if platform, handle, ok := socialRefFromURL(target.ContactURL); ok {
			target.SocialPlatform = platform
			if target.SocialHandle == "" {
				target.SocialHandle = handle
			}
		}
	}
	if target.Channel == "website" && target.ContactURL == "" {
		target.ContactURL = target.Website
	}
	if !hasCaseDiscoveryContactMethod(target) {
		return models.CaseEscalationTarget{}, false
	}
	return target, true
}

func mergeCaseEscalationTargets(existing, incoming models.CaseEscalationTarget) models.CaseEscalationTarget {
	primary, secondary := existing, incoming
	if incoming.ConfidenceScore > existing.ConfidenceScore {
		primary, secondary = incoming, existing
	}
	primary.RoleType = firstNonEmpty(primary.RoleType, secondary.RoleType)
	primary.Organization = firstNonEmpty(primary.Organization, secondary.Organization)
	primary.DisplayName = firstNonEmpty(primary.DisplayName, secondary.DisplayName)
	primary.Channel = firstNonEmpty(primary.Channel, secondary.Channel)
	primary.Email = firstNonEmpty(primary.Email, secondary.Email)
	primary.Phone = firstNonEmpty(primary.Phone, secondary.Phone)
	primary.Website = firstNonEmpty(primary.Website, secondary.Website)
	primary.ContactURL = firstNonEmpty(primary.ContactURL, secondary.ContactURL)
	primary.SocialPlatform = firstNonEmpty(primary.SocialPlatform, secondary.SocialPlatform)
	primary.SocialHandle = firstNonEmpty(primary.SocialHandle, secondary.SocialHandle)
	primary.TargetSource = firstNonEmpty(primary.TargetSource, secondary.TargetSource)
	primary.Rationale = mergeRationale(primary.Rationale, secondary.Rationale)
	return primary
}

func mergeRationale(primary, secondary string) string {
	primary = strings.TrimSpace(primary)
	secondary = strings.TrimSpace(secondary)
	if primary == "" {
		return secondary
	}
	if secondary == "" || secondary == primary {
		return primary
	}
	if strings.Contains(primary, secondary) {
		return primary
	}
	return primary + " | " + secondary
}

func caseEscalationTargetKey(target models.CaseEscalationTarget) string {
	switch target.Channel {
	case "email":
		if target.Email == "" {
			return ""
		}
		return "email:" + strings.ToLower(target.Email)
	case "phone":
		if target.Phone == "" {
			return ""
		}
		return "phone:" + phoneDigits(target.Phone)
	case "social":
		if target.SocialPlatform == "" && target.ContactURL != "" {
			if platform, handle, ok := socialRefFromURL(target.ContactURL); ok {
				target.SocialPlatform, target.SocialHandle = platform, handle
			}
		}
		if target.SocialPlatform == "" || target.SocialHandle == "" {
			return ""
		}
		return "social:" + target.SocialPlatform + ":" + strings.ToLower(target.SocialHandle)
	case "website":
		key := canonicalURLKey(firstNonEmpty(target.ContactURL, target.Website))
		if key == "" {
			return ""
		}
		return "website:" + key
	default:
		if target.Email != "" {
			return "email:" + strings.ToLower(target.Email)
		}
		if target.Phone != "" {
			return "phone:" + phoneDigits(target.Phone)
		}
		if target.SocialHandle != "" && target.SocialPlatform != "" {
			return "social:" + target.SocialPlatform + ":" + strings.ToLower(target.SocialHandle)
		}
		key := canonicalURLKey(firstNonEmpty(target.ContactURL, target.Website))
		if key == "" {
			return ""
		}
		return "website:" + key
	}
}

func caseChannelRank(channel string) int {
	switch channel {
	case "email":
		return 0
	case "phone":
		return 1
	case "social":
		return 2
	case "website":
		return 3
	default:
		return 4
	}
}

func hasClassToken(node *xhtml.Node, token string) bool {
	classValue := attrValue(node, "class")
	if classValue == "" {
		return false
	}
	for _, value := range strings.Fields(classValue) {
		if strings.EqualFold(strings.TrimSpace(value), token) {
			return true
		}
	}
	return false
}

func findDescendantByClass(node *xhtml.Node, classToken string) *xhtml.Node {
	if node == nil {
		return nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && hasClassToken(child, classToken) {
			return child
		}
		if found := findDescendantByClass(child, classToken); found != nil {
			return found
		}
	}
	return nil
}

func attrValue(node *xhtml.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func nodeText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.TextNode {
		return node.Data
	}
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text := strings.TrimSpace(nodeText(child))
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(text)
	}
	return b.String()
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base))
	for _, value := range base {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		base = append(base, trimmed)
	}
	return base
}

func appendUniqueSocials(base []caseSocialRef, value caseSocialRef) []caseSocialRef {
	if value.Platform == "" || value.Handle == "" {
		return base
	}
	key := strings.ToLower(value.Platform + ":" + value.Handle)
	for _, existing := range base {
		if strings.ToLower(existing.Platform+":"+existing.Handle) == key {
			return base
		}
	}
	return append(base, value)
}

func extractEmailsFromHTML(html string) []string {
	emails := []string{}
	for _, match := range caseDiscoveryMailtoRegex.FindAllStringSubmatch(html, -1) {
		if len(match) > 1 {
			emails = appendUniqueStrings(emails, normalizeEmail(match[1]))
		}
	}
	for _, match := range caseDiscoveryEmailRegex.FindAllStringSubmatch(html, -1) {
		if len(match) > 1 {
			emails = appendUniqueStrings(emails, normalizeEmail(match[1]))
		}
	}
	out := make([]string, 0, len(emails))
	for _, email := range emails {
		if email != "" {
			out = append(out, email)
		}
	}
	return out
}

func extractPhonesFromHTML(html string) []string {
	phones := []string{}
	for _, match := range caseDiscoveryTelRegex.FindAllStringSubmatch(html, -1) {
		if len(match) > 1 {
			phones = appendUniqueStrings(phones, normalizePhone(match[1]))
		}
	}
	for _, match := range caseDiscoveryPhoneRegex.FindAllString(html, -1) {
		phones = appendUniqueStrings(phones, normalizePhone(match))
	}
	out := make([]string, 0, len(phones))
	for _, phone := range phones {
		if phone != "" {
			out = append(out, phone)
		}
	}
	return out
}

func extractSocialRefsFromHTML(html string) []caseSocialRef {
	refs := []caseSocialRef{}
	for _, match := range caseDiscoveryHrefRegex.FindAllStringSubmatch(html, -1) {
		if len(match) < 2 {
			continue
		}
		if platform, handle, ok := socialRefFromURL(match[1]); ok {
			refs = appendUniqueSocials(refs, caseSocialRef{Platform: platform, Handle: handle, URL: match[1]})
		}
	}
	return refs
}

func extractContactLinks(base *url.URL, html string) []string {
	links := []string{}
	for _, match := range caseDiscoveryHrefRegex.FindAllStringSubmatch(html, -1) {
		if len(match) < 2 {
			continue
		}
		href := strings.TrimSpace(match[1])
		lower := strings.ToLower(href)
		if !(strings.Contains(lower, "contact") || strings.Contains(lower, "about") || strings.Contains(lower, "impressum") || strings.Contains(lower, "support") || strings.Contains(lower, "directory")) {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(parsed)
		if !sameHost(base, resolved) {
			continue
		}
		links = appendUniqueStrings(links, resolved.String())
	}
	if len(links) > 2 {
		links = links[:2]
	}
	return links
}

func socialRefsFromTags(tags map[string]string) []caseSocialRef {
	refs := []caseSocialRef{}
	for key, value := range tags {
		platform := ""
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "contact:facebook", "facebook":
			platform = "facebook"
		case "contact:instagram", "instagram":
			platform = "instagram"
		case "contact:twitter", "twitter", "contact:x", "x":
			platform = "x"
		case "contact:linkedin", "linkedin":
			platform = "linkedin"
		case "contact:tiktok", "tiktok":
			platform = "tiktok"
		}
		if platform == "" {
			continue
		}
		ref, ok := normalizeSocialRef(platform, value)
		if ok {
			refs = appendUniqueSocials(refs, ref)
		}
	}
	return refs
}

func normalizeSocialRef(platform, raw string) (caseSocialRef, bool) {
	platform = normalizeSocialPlatform(platform)
	raw = strings.TrimSpace(raw)
	if platform == "" || raw == "" {
		return caseSocialRef{}, false
	}
	if strings.Contains(raw, "://") {
		if inferredPlatform, handle, ok := socialRefFromURL(raw); ok {
			if platform == "" {
				platform = inferredPlatform
			}
			return caseSocialRef{Platform: inferredPlatform, Handle: handle, URL: raw}, true
		}
	}
	handle := normalizeSocialHandle(raw)
	if handle == "" {
		return caseSocialRef{}, false
	}
	return caseSocialRef{Platform: platform, Handle: handle, URL: buildSocialURL(platform, handle)}, true
}

func socialRefFromURL(raw string) (string, string, bool) {
	if raw == "" {
		return "", "", false
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + strings.TrimLeft(raw, "/")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	pathParts := strings.FieldsFunc(strings.Trim(parsed.Path, "/"), func(r rune) bool { return r == '/' })
	if len(pathParts) == 0 {
		return "", "", false
	}
	handle := normalizeSocialHandle(pathParts[0])
	if handle == "" {
		return "", "", false
	}
	switch {
	case strings.Contains(host, "twitter.com"), strings.Contains(host, "x.com"):
		return "x", handle, true
	case strings.Contains(host, "instagram.com"):
		return "instagram", handle, true
	case strings.Contains(host, "facebook.com"):
		return "facebook", handle, true
	case strings.Contains(host, "linkedin.com"):
		return "linkedin", handle, true
	case strings.Contains(host, "tiktok.com"):
		return "tiktok", handle, true
	default:
		return "", "", false
	}
}

func normalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	if !caseDiscoveryEmailRegex.MatchString(email) {
		return ""
	}
	return email
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	digits := phoneDigits(phone)
	if len(digits) < 7 {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(phone), "+") {
		return "+" + digits
	}
	return digits
}

func phoneDigits(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeWebsiteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "mailto:") || strings.HasPrefix(strings.ToLower(raw), "tel:") {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String()
}

func normalizeFlexibleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "tel:") {
		return raw
	}
	return normalizeWebsiteURL(raw)
}

func canonicalURLKey(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname() + strings.TrimSuffix(parsed.EscapedPath(), "/"))
}

func normalizeSocialPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "twitter", "x":
		return "x"
	case "instagram":
		return "instagram"
	case "facebook":
		return "facebook"
	case "linkedin":
		return "linkedin"
	case "tiktok":
		return "tiktok"
	default:
		return strings.ToLower(strings.TrimSpace(platform))
	}
}

func normalizeSocialHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	handle = strings.TrimPrefix(handle, "@")
	handle = strings.Trim(handle, "/")
	if handle == "" {
		return ""
	}
	return handle
}

func buildSocialURL(platform, handle string) string {
	switch normalizeSocialPlatform(platform) {
	case "x":
		return "https://x.com/" + handle
	case "instagram":
		return "https://www.instagram.com/" + handle
	case "facebook":
		return "https://www.facebook.com/" + handle
	case "linkedin":
		return "https://www.linkedin.com/in/" + handle
	case "tiktok":
		return "https://www.tiktok.com/@" + handle
	default:
		return ""
	}
}

func sameHost(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname())
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}
