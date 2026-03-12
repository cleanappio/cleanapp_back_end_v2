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
	caseDiscoveryGoogleSearchURL = "https://www.googleapis.com/customsearch/v1"
	caseDiscoveryMaxWebsiteFetch = 3
)

var (
	caseDiscoveryMailtoRegex        = regexp.MustCompile(`(?i)mailto:([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})`)
	caseDiscoveryEmailRegex         = regexp.MustCompile(`(?i)\b([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})\b`)
	caseDiscoveryTelRegex           = regexp.MustCompile(`(?i)tel:([^"'<>]+)`)
	caseDiscoveryPhoneRegex         = regexp.MustCompile(`\+?[0-9][0-9()\-\.\s]{6,}[0-9]`)
	caseDiscoveryTimeRangeRegex     = regexp.MustCompile(`^\s*\d{1,2}[\.:]\d{2}\s*[-–]\s*\d{1,2}[\.:]\d{2}\s*$`)
	caseDiscoveryDateRangeRegex     = regexp.MustCompile(`^\s*(?:19|20)\d{2}\s*[-/]\s*(?:19|20)\d{2}\s*$`)
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
	caseDiscoveryPhoneContextKeywords = []string{
		"tel", "telefon", "phone", "call", "hotline", "switchboard", "reception",
		"kontakt", "contact", "office", "administration", "verwaltung", "zentrale",
	}
)

type caseContactDiscoverer struct {
	httpClient          *http.Client
	googlePlacesAPIKey  string
	googlePlacesBaseURL string
	googleSearchAPIKey  string
	googleSearchCX      string
	googleSearchBaseURL string
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
	Contacts     []caseDiscoveredContact
}

type caseStakeholderSearchQuery struct {
	RoleType        string
	Query           string
	Region          string
	BaseConfidence  float64
	Rationale       string
	Organization    string
	RelationshipTag string
	RequireOfficial bool
}

type caseWebSearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
}

type caseHazardProfile struct {
	Structural         bool
	Severe             bool
	Urgent             bool
	ImmediateDanger    bool
	SensitiveOccupancy bool
}

type caseDiscoveredContact struct {
	Channel           string
	Email             string
	Phone             string
	Social            caseSocialRef
	SourceURL         string
	EvidenceText      string
	VerificationLevel string
	RoleHint          string
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
		httpClient:          &http.Client{Timeout: 4 * time.Second},
		googlePlacesAPIKey:  strings.TrimSpace(cfg.GooglePlacesAPIKey),
		googlePlacesBaseURL: strings.TrimRight(strings.TrimSpace(cfg.GooglePlacesBaseURL), "/"),
		googleSearchAPIKey:  strings.TrimSpace(cfg.GoogleSearchAPIKey),
		googleSearchCX:      strings.TrimSpace(cfg.GoogleSearchCX),
		googleSearchBaseURL: strings.TrimRight(strings.TrimSpace(cfg.GoogleSearchBaseURL), "/"),
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
	existingWebsiteSeeds := collectExistingWebsiteSeeds(existing)
	hydratedExistingWebsites := false
	visitedWebsites := make(map[string]struct{})
	searchedQueries := make(map[string]struct{})
	for _, target := range existing {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			inferredFallback = append(inferredFallback, target)
			continue
		}
		merger.Add(target)
	}
	if len(reports) == 0 {
		for _, seed := range existingWebsiteSeeds {
			if ctx.Err() != nil {
				break
			}
			d.addWebsiteTargets(
				ctx,
				seed.RoleType,
				seed.Organization,
				seed.Website,
				seed.ContactURL,
				seed.Source,
				seed.BaseConfidence,
				seed.Rationale,
				merger,
				visitedWebsites,
			)
		}
		if countPreferredCaseTargets(merger.Targets()) == 0 {
			for _, target := range inferredFallback {
				merger.Add(target)
			}
		}
		return merger.Targets()
	}

	hazardProfile := analyzeCaseHazardProfile(reports)
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
		if hazardProfile.Structural &&
			(hazardProfile.Severe || hazardProfile.ImmediateDanger || hazardProfile.SensitiveOccupancy) &&
			(d.webSearchBaseURL != "" || (d.googleSearchAPIKey != "" && d.googleSearchCX != "")) &&
			hasRemainingContactDiscoveryBudget(ctx, 4*time.Second) &&
			shouldRunStakeholderWebSearch(merger.Targets(), hazardProfile) {
			queries := buildCaseStakeholderSearchQueries(candidateNames, locCtx, hazardProfile)
			d.addWebSearchStakeholderTargets(ctx, queries, merger, visitedWebsites, searchedQueries)
		}
		if !hydratedExistingWebsites {
			hydratedExistingWebsites = true
			for _, websiteSeed := range existingWebsiteSeeds {
				if ctx.Err() != nil {
					break
				}
				d.addWebsiteTargets(
					ctx,
					websiteSeed.RoleType,
					websiteSeed.Organization,
					websiteSeed.Website,
					websiteSeed.ContactURL,
					websiteSeed.Source,
					websiteSeed.BaseConfidence,
					websiteSeed.Rationale,
					merger,
					visitedWebsites,
				)
			}
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

		if (d.webSearchBaseURL != "" || (d.googleSearchAPIKey != "" && d.googleSearchCX != "")) &&
			hasRemainingContactDiscoveryBudget(ctx, 2*time.Second) &&
			shouldRunStakeholderWebSearch(merger.Targets(), hazardProfile) {
			queries := buildCaseStakeholderSearchQueries(candidateNames, locCtx, hazardProfile)
			d.addWebSearchStakeholderTargets(ctx, queries, merger, visitedWebsites, searchedQueries)
		}
		if !hasRemainingContactDiscoveryBudget(ctx, time.Second) {
			break
		}
	}
	if !hydratedExistingWebsites {
		for _, websiteSeed := range existingWebsiteSeeds {
			if ctx.Err() != nil {
				break
			}
			d.addWebsiteTargets(
				ctx,
				websiteSeed.RoleType,
				websiteSeed.Organization,
				websiteSeed.Website,
				websiteSeed.ContactURL,
				websiteSeed.Source,
				websiteSeed.BaseConfidence,
				websiteSeed.Rationale,
				merger,
				visitedWebsites,
			)
		}
	}

	if countPreferredCaseTargets(merger.Targets()) == 0 {
		for _, target := range inferredFallback {
			merger.Add(target)
		}
	}
	return merger.Targets()
}

type existingWebsiteSeed struct {
	RoleType       string
	Organization   string
	Website        string
	ContactURL     string
	Source         string
	BaseConfidence float64
	Rationale      string
}

func collectExistingWebsiteSeeds(existing []models.CaseEscalationTarget) []existingWebsiteSeed {
	seeds := make([]existingWebsiteSeed, 0, len(existing))
	seen := make(map[string]struct{})
	for _, target := range existing {
		website, contactURL := existingTargetWebsiteSeed(target)
		if website == "" && contactURL == "" {
			continue
		}
		roleType := emptyDefault(strings.TrimSpace(target.RoleType), "operator")
		organization := firstNonEmpty(strings.TrimSpace(target.Organization), strings.TrimSpace(target.DisplayName))
		if organization == "" {
			organization = "Official stakeholder"
		}
		source := emptyDefault(strings.TrimSpace(target.TargetSource), "saved_case_target")
		baseConfidence := target.ConfidenceScore
		if baseConfidence <= 0 {
			baseConfidence = 0.74
		}
		key := strings.Join([]string{
			strings.ToLower(roleType),
			strings.ToLower(organization),
			canonicalURLKey(firstNonEmpty(contactURL, website)),
		}, "|")
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		seeds = append(seeds, existingWebsiteSeed{
			RoleType:       roleType,
			Organization:   organization,
			Website:        firstNonEmpty(website, contactURL),
			ContactURL:     firstNonEmpty(contactURL, website),
			Source:         source,
			BaseConfidence: maxFloat(baseConfidence, 0.72),
			Rationale:      fmt.Sprintf("Official website previously associated with %s.", organization),
		})
	}
	return seeds
}

func existingTargetWebsiteSeed(target models.CaseEscalationTarget) (string, string) {
	candidates := []string{
		strings.TrimSpace(target.Website),
		strings.TrimSpace(target.ContactURL),
		strings.TrimSpace(target.SourceURL),
	}
	legacyEmail := strings.TrimSpace(target.Email)
	if legacyEmail != "" && normalizeEmail(legacyEmail) == "" {
		candidates = append(candidates, legacyEmail)
	}

	var website string
	var contactURL string
	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		if contactURL == "" {
			contactURL = normalizeFlexibleURL(raw)
		}
		if website == "" {
			website = normalizeWebsiteURL(raw)
		}
	}
	return website, firstNonEmpty(contactURL, website)
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
			)
			if preferredClassification(&report) == "digital" {
				names = appendUniqueStrings(names, strings.TrimSpace(analysis.Title))
			}
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

func analyzeCaseHazardProfile(reports []models.ReportWithAnalysis) caseHazardProfile {
	structuralKeywords := []string{
		"structural", "crack", "cracking", "brick", "bricks", "facade", "façade", "masonry",
		"wall", "concrete", "beam", "column", "roof", "ceiling", "collapse", "falling",
		"exterior", "foundation", "balcony", "spalling", "detachment", "separation",
		"fissure", "fassade", "fassaden", "mauerwerk", "beton", "riss", "risse",
		"support column", "pillar", "platform edge", "station roof", "overhang",
	}
	dangerKeywords := []string{
		"falling", "collapse", "imminent", "exposed", "detached", "separating",
		"hazard", "danger", "unsafe", "critical", "life safety", "major failure",
		"falling object", "falling debris", "urgent", "emergency",
	}
	sensitiveOccupancyKeywords := []string{
		"school", "primary school", "kindergarten", "nursery", "hospital", "clinic",
		"metro", "subway", "station", "platform", "terminal", "playground", "campus",
		"children", "students", "passengers", "commuters", "public hall", "arena",
		"mall", "daycare", "care home", "stadium",
	}

	profile := caseHazardProfile{}
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
			analysis.BrandDisplayName,
			analysis.BrandName,
		}, " "))

		structuralHits := 0
		for _, keyword := range structuralKeywords {
			if strings.Contains(text, keyword) {
				structuralHits++
			}
		}
		if structuralHits >= 2 || (structuralHits >= 1 && analysis.SeverityLevel >= 0.75) {
			profile.Structural = true
		}
		if analysis.SeverityLevel >= 0.85 {
			profile.Severe = true
		}
		if analysis.HazardProbability >= 0.75 || analysis.SeverityLevel >= 0.9 {
			profile.Urgent = true
		}
		if containsAny(text, dangerKeywords) && (analysis.SeverityLevel >= 0.7 || structuralHits >= 2) {
			profile.ImmediateDanger = true
		}
		if containsAny(text, sensitiveOccupancyKeywords) {
			profile.SensitiveOccupancy = true
		}
	}
	return profile
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

func countActionableDirectCaseTargets(targets []models.CaseEscalationTarget) int {
	count := 0
	for _, target := range targets {
		if strings.EqualFold(strings.TrimSpace(target.TargetSource), "inferred_contact") {
			continue
		}
		if normalizeEmail(target.Email) != "" || normalizePhone(target.Phone) != "" || strings.TrimSpace(target.SocialHandle) != "" {
			count++
		}
	}
	return count
}

func shouldRunStakeholderWebSearch(targets []models.CaseEscalationTarget, hazard caseHazardProfile) bool {
	directCount := countActionableDirectCaseTargets(targets)
	if directCount == 0 {
		return true
	}
	if hazard.Structural && (hazard.Severe || hazard.ImmediateDanger) {
		if !hasCaseRoleTarget(targets, "building_authority") {
			return true
		}
		if hazard.SensitiveOccupancy && !hasCaseRoleTarget(targets, "public_safety", "fire_authority") {
			return true
		}
		return directCount < 4
	}
	return directCount < 3
}

func hasRemainingContactDiscoveryBudget(ctx context.Context, minimum time.Duration) bool {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline) > minimum
	}
	return true
}

func hasCaseRoleTarget(targets []models.CaseEscalationTarget, roles ...string) bool {
	roleSet := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "" {
			roleSet[role] = struct{}{}
		}
	}
	for _, target := range targets {
		role := strings.ToLower(strings.TrimSpace(target.RoleType))
		if _, ok := roleSet[role]; ok {
			return true
		}
	}
	return false
}

func buildCaseStakeholderSearchQueries(candidateNames []string, locCtx *caseLocationContext, hazard caseHazardProfile) []caseStakeholderSearchQuery {
	primaryName := ""
	if locCtx != nil {
		primaryName = firstNonEmpty(locCtx.PrimaryName, locCtx.ParentOrg, locCtx.Operator)
	}
	if primaryName == "" {
		primaryName = bestStakeholderNameCandidate(candidateNames)
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
			RequireOfficial: true,
		},
	}

	if hazard.Structural {
		criticalStructuralSearch := hazard.Severe || hazard.ImmediateDanger || hazard.SensitiveOccupancy
		facilityTerm := "facility management"
		if isGermanSpeakingLocation(locCtx) {
			facilityTerm = "hausdienst"
		}
		facilityQuery := caseStakeholderSearchQuery{
			RoleType:        "facility_manager",
			Query:           strings.TrimSpace(baseQuery + " " + facilityTerm),
			Region:          region,
			BaseConfidence:  0.8,
			Rationale:       "Operational or facilities contact search for the affected site.",
			Organization:    primaryName,
			RelationshipTag: "operations",
			RequireOfficial: true,
		}
		if !criticalStructuralSearch {
			queries = append(queries, facilityQuery)
		}
	}

	if !hazard.Structural {
		return queries
	}

	architectTerm := "architect"
	contractorTerm := "contractor"
	engineerTerm := "structural engineer"
	authorityTerm := "building department"
	fireAuthorityTerm := "fire marshal"
	publicSafetyTerm := "public safety"
	if isGermanSpeakingLocation(locCtx) {
		architectTerm = "architekt"
		contractorTerm = "bauunternehmung"
		engineerTerm = "ingenieur"
		authorityTerm = "bauamt"
		fireAuthorityTerm = "feuerpolizei"
		publicSafetyTerm = "polizei"
	}

	if locality != "" {
		queries = append(queries, caseStakeholderSearchQuery{
			RoleType:        "building_authority",
			Query:           strings.TrimSpace(quotedSearchPhrase(locality) + " " + authorityTerm),
			Region:          region,
			BaseConfidence:  0.84,
			Rationale:       "Web search for the local building authority or municipal office responsible for the affected site.",
			Organization:    locality,
			RelationshipTag: "authority",
			RequireOfficial: true,
		})
		if hazard.Severe || hazard.Urgent || hazard.ImmediateDanger {
			queries = append(queries, caseStakeholderSearchQuery{
				RoleType:        "fire_authority",
				Query:           strings.TrimSpace(quotedSearchPhrase(locality) + " " + fireAuthorityTerm),
				Region:          region,
				BaseConfidence:  0.8,
				Rationale:       "Web search for the local fire/building-safety authority relevant to a severe structural hazard.",
				Organization:    locality,
				RelationshipTag: "fire authority",
				RequireOfficial: true,
			})
		}
		if hazard.ImmediateDanger || (hazard.SensitiveOccupancy && hazard.Severe) {
			queries = append(queries, caseStakeholderSearchQuery{
				RoleType:        "public_safety",
				Query:           strings.TrimSpace(quotedSearchPhrase(locality) + " " + publicSafetyTerm),
				Region:          region,
				BaseConfidence:  0.76,
				Rationale:       "Web search for public-safety contacts relevant to an immediate life-safety hazard at the site.",
				Organization:    locality,
				RelationshipTag: "public safety",
				RequireOfficial: true,
			})
		}
		if hazard.Severe || hazard.ImmediateDanger || hazard.SensitiveOccupancy {
			facilityTerm := "facility management"
			if isGermanSpeakingLocation(locCtx) {
				facilityTerm = "hausdienst"
			}
			queries = append(queries, caseStakeholderSearchQuery{
				RoleType:        "facility_manager",
				Query:           strings.TrimSpace(baseQuery + " " + facilityTerm),
				Region:          region,
				BaseConfidence:  0.8,
				Rationale:       "Operational or facilities contact search for the affected site.",
				Organization:    primaryName,
				RelationshipTag: "operations",
				RequireOfficial: true,
			})
		}
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
	}

	return queries
}

func bestStakeholderNameCandidate(candidateNames []string) string {
	for _, candidate := range candidateNames {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || isLikelyIncidentDescriptorName(candidate) {
			continue
		}
		return candidate
	}
	return firstNonEmpty(candidateNames...)
}

func isLikelyIncidentDescriptorName(candidate string) bool {
	lower := strings.ToLower(strings.TrimSpace(candidate))
	if lower == "" {
		return false
	}
	incidentTokens := []string{
		"hazard", "incident", "defect", "damage", "failure", "crack", "cracking",
		"structural", "unsafe", "danger", "collapse", "debris", "falling",
		"obstruction", "leak", "litter", "trash", "graffiti", "pothole", "bug",
		"vulnerability", "outage", "exposed", "deterioration", "anomaly",
	}
	hits := 0
	for _, token := range incidentTokens {
		if strings.Contains(lower, token) {
			hits++
		}
	}
	return hits >= 2
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
			SourceURL:       locCtx.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       locCtx.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       locCtx.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       poi.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       poi.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       poi.Website,
			Verification:    "openstreetmap",
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
			SourceURL:       firstNonEmpty(place.GoogleMapsURI, place.WebsiteURI),
			Verification:    "directory_listing",
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
	queries = pendingStakeholderSearchQueries(merger.Targets(), queries)
	searchCount := 0
	for _, query := range queries {
		if searchCount >= 6 {
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
		searchCount++

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
			if query.RequireOfficial && !isLikelyOfficialStakeholderResult(result.URL, query) {
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

func pendingStakeholderSearchQueries(targets []models.CaseEscalationTarget, queries []caseStakeholderSearchQuery) []caseStakeholderSearchQuery {
	if len(queries) == 0 {
		return nil
	}
	pending := make([]caseStakeholderSearchQuery, 0, len(queries))
	for _, query := range queries {
		if hasCaseRoleTarget(targets, query.RoleType) {
			continue
		}
		pending = append(pending, query)
	}
	return pending
}

func (d *caseContactDiscoverer) searchStakeholderWeb(ctx context.Context, query, region string) ([]caseWebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if d.googleSearchAPIKey != "" && d.googleSearchCX != "" {
		results, err := d.searchGoogleCustomSearch(ctx, query)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		if err != nil {
			log.Printf("warn: google custom search failed for %q: %v", query, err)
		}
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

type googleCustomSearchResponse struct {
	Items []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"items"`
}

func (d *caseContactDiscoverer) searchGoogleCustomSearch(ctx context.Context, query string) ([]caseWebSearchResult, error) {
	searchURL := d.googleSearchBaseURL
	if searchURL == "" {
		searchURL = caseDiscoveryGoogleSearchURL
	}
	params := url.Values{}
	params.Set("key", d.googleSearchAPIKey)
	params.Set("cx", d.googleSearchCX)
	params.Set("q", query)
	params.Set("num", "5")

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
		return nil, fmt.Errorf("google custom search returned http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded googleCustomSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	results := make([]caseWebSearchResult, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		resolved := normalizeWebsiteURL(item.Link)
		if resolved == "" {
			continue
		}
		results = append(results, caseWebSearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     resolved,
			Snippet: strings.TrimSpace(item.Snippet),
		})
	}
	return results, nil
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
			for _, discovered := range contacts.Contacts {
				resolvedRole := mergeDiscoveredRoleType(roleType, discovered.RoleHint)
				targetSource := source + "_website"
				evidenceText := strings.TrimSpace(discovered.EvidenceText)
				sourceURL := firstNonEmpty(discovered.SourceURL, contactURL, normalizedWebsite)
				verificationLevel := firstNonEmpty(
					discovered.VerificationLevel,
					inferVerificationLevel(targetSource, resolvedRole, normalizedWebsite, sourceURL),
				)
				switch discovered.Channel {
				case "email":
					if discovered.Email == "" {
						continue
					}
					if !shouldKeepWebsiteDiscoveredContact(resolvedRole, organization, normalizedWebsite, discovered) {
						continue
					}
					merger.Add(models.CaseEscalationTarget{
						RoleType:        resolvedRole,
						Organization:    organization,
						DisplayName:     organization,
						Channel:         "email",
						Email:           discovered.Email,
						Website:         normalizedWebsite,
						ContactURL:      contactURL,
						SourceURL:       sourceURL,
						EvidenceText:    evidenceText,
						Verification:    verificationLevel,
						TargetSource:    targetSource,
						ConfidenceScore: baseConfidence,
						Rationale:       fmt.Sprintf("Email scraped from the official website for %s.", organization),
					})
				case "phone":
					if discovered.Phone == "" {
						continue
					}
					merger.Add(models.CaseEscalationTarget{
						RoleType:        resolvedRole,
						Organization:    organization,
						DisplayName:     organization,
						Channel:         "phone",
						Phone:           discovered.Phone,
						Website:         normalizedWebsite,
						ContactURL:      contactURL,
						SourceURL:       sourceURL,
						EvidenceText:    evidenceText,
						Verification:    verificationLevel,
						TargetSource:    targetSource,
						ConfidenceScore: baseConfidence - 0.02,
						Rationale:       fmt.Sprintf("Phone number scraped from the official website for %s.", organization),
					})
				case "social":
					if discovered.Social.Platform == "" || discovered.Social.Handle == "" {
						continue
					}
					merger.Add(models.CaseEscalationTarget{
						RoleType:        resolvedRole,
						Organization:    organization,
						DisplayName:     organization,
						Channel:         "social",
						Website:         normalizedWebsite,
						ContactURL:      discovered.Social.URL,
						SourceURL:       sourceURL,
						EvidenceText:    evidenceText,
						Verification:    verificationLevel,
						SocialPlatform:  discovered.Social.Platform,
						SocialHandle:    discovered.Social.Handle,
						TargetSource:    targetSource,
						ConfidenceScore: baseConfidence - 0.05,
						Rationale:       fmt.Sprintf("Social profile linked from the official website for %s.", organization),
					})
				}
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
		SourceURL:       firstNonEmpty(contactURL, normalizedWebsite),
		Verification:    inferVerificationLevel(source, roleType, normalizedWebsite, firstNonEmpty(contactURL, normalizedWebsite)),
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
	for _, candidate := range []string{"/contact", "/contact-us", "/about", "/impressum", "/team", "/directory"} {
		queue = append(queue, parsedBase.ResolveReference(&url.URL{Path: candidate}).String())
	}

	for i := 0; i < len(queue) && len(fetched) < caseDiscoveryMaxWebsiteFetch+2; i++ {
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
		artifacts := extractDiscoveredContacts(finalURL, htmlBody)
		result.Contacts = appendUniqueDiscoveredContacts(result.Contacts, artifacts...)
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
	target.DecisionScope = strings.TrimSpace(target.DecisionScope)
	target.Organization = strings.TrimSpace(target.Organization)
	target.DisplayName = strings.TrimSpace(target.DisplayName)
	rawEmail := strings.TrimSpace(target.Email)
	rawPhone := strings.TrimSpace(target.Phone)
	target.Email = normalizeEmail(target.Email)
	target.Phone = normalizePhone(target.Phone)
	if target.Phone == "" && strings.EqualFold(strings.TrimSpace(target.Channel), "phone") {
		target.Channel = ""
	}
	target.Website = normalizeWebsiteURL(target.Website)
	target.ContactURL = normalizeFlexibleURL(target.ContactURL)
	target.SourceURL = normalizeFlexibleURL(target.SourceURL)
	target.EvidenceText = compactWhitespace(target.EvidenceText)
	target.Verification = strings.TrimSpace(target.Verification)
	target.AttributionClass = strings.TrimSpace(target.AttributionClass)
	target.SocialPlatform = normalizeSocialPlatform(target.SocialPlatform)
	target.SocialHandle = normalizeSocialHandle(target.SocialHandle)
	target.TargetSource = emptyDefault(strings.TrimSpace(target.TargetSource), "suggested")
	target.SendEligibility = strings.TrimSpace(target.SendEligibility)
	target.ReasonSelected = strings.TrimSpace(target.ReasonSelected)
	target.Rationale = strings.TrimSpace(target.Rationale)
	if target.Email != "" && !shouldKeepWebsiteDiscoveredContact(target.RoleType, firstNonEmpty(target.Organization, target.DisplayName), firstNonEmpty(target.Website, target.SourceURL), caseDiscoveredContact{
		Channel: "email",
		Email:   target.Email,
	}) {
		target.Email = ""
		if strings.EqualFold(strings.TrimSpace(target.Channel), "email") {
			target.Channel = ""
		}
	}
	if target.Email == "" && rawEmail != "" && normalizeEmail(rawEmail) == "" {
		if recoveredWebsite := normalizeWebsiteURL(rawEmail); recoveredWebsite != "" {
			target.Website = firstNonEmpty(target.Website, recoveredWebsite)
			target.ContactURL = firstNonEmpty(target.ContactURL, normalizeFlexibleURL(rawEmail), recoveredWebsite)
			if strings.TrimSpace(target.Channel) == "" || strings.EqualFold(strings.TrimSpace(target.Channel), "email") {
				target.Channel = "website"
			}
		}
	}
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
	if target.Channel == "" {
		target.Channel = caseDiscoveryTargetChannel(target)
	}
	if target.Phone != "" && !looksLikePublishedPhone(rawPhone, target.Phone, target.EvidenceText) {
		target.Phone = ""
		if target.Channel == "phone" {
			target.Channel = ""
		}
	}
	if target.Channel == "" {
		target.Channel = caseDiscoveryTargetChannel(target)
	}
	if target.SourceURL == "" {
		target.SourceURL = firstNonEmpty(target.ContactURL, target.Website)
	}
	if target.Verification == "" {
		target.Verification = inferVerificationLevel(target.TargetSource, target.RoleType, target.Website, target.SourceURL)
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
	primary.DecisionScope = firstNonEmpty(primary.DecisionScope, secondary.DecisionScope)
	primary.Organization = firstNonEmpty(primary.Organization, secondary.Organization)
	primary.DisplayName = firstNonEmpty(primary.DisplayName, secondary.DisplayName)
	primary.Channel = firstNonEmpty(primary.Channel, secondary.Channel)
	primary.Email = firstNonEmpty(primary.Email, secondary.Email)
	primary.Phone = firstNonEmpty(primary.Phone, secondary.Phone)
	primary.Website = firstNonEmpty(primary.Website, secondary.Website)
	primary.ContactURL = firstNonEmpty(primary.ContactURL, secondary.ContactURL)
	primary.SourceURL = firstNonEmpty(primary.SourceURL, secondary.SourceURL)
	primary.EvidenceText = firstNonEmpty(primary.EvidenceText, secondary.EvidenceText)
	primary.Verification = firstNonEmpty(primary.Verification, secondary.Verification)
	primary.AttributionClass = firstNonEmpty(primary.AttributionClass, secondary.AttributionClass)
	primary.SocialPlatform = firstNonEmpty(primary.SocialPlatform, secondary.SocialPlatform)
	primary.SocialHandle = firstNonEmpty(primary.SocialHandle, secondary.SocialHandle)
	primary.TargetSource = firstNonEmpty(primary.TargetSource, secondary.TargetSource)
	if secondary.ActionabilityScore > primary.ActionabilityScore {
		primary.ActionabilityScore = secondary.ActionabilityScore
	}
	if primary.NotifyTier == 0 || (secondary.NotifyTier > 0 && secondary.NotifyTier < primary.NotifyTier) {
		primary.NotifyTier = secondary.NotifyTier
	}
	primary.SendEligibility = firstNonEmpty(primary.SendEligibility, secondary.SendEligibility)
	primary.ReasonSelected = firstNonEmpty(primary.ReasonSelected, secondary.ReasonSelected)
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

func shouldKeepWebsiteDiscoveredContact(roleType, organization, websiteURL string, discovered caseDiscoveredContact) bool {
	if discovered.Channel != "email" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(roleType)) {
	case "architect", "contractor", "engineer":
		return true
	default:
		return emailMatchesWebsiteOrOrganization(discovered.Email, websiteURL, organization)
	}
}

func emailMatchesWebsiteOrOrganization(email, websiteURL, organization string) bool {
	email = normalizeEmail(email)
	if email == "" {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	emailDomain := strings.ToLower(email[at+1:])
	websiteHost := ""
	if parsed, err := url.Parse(firstNonEmpty(websiteURL, "https://"+emailDomain)); err == nil {
		websiteHost = strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	}
	if websiteHost == "" {
		return true
	}
	if emailDomain == websiteHost || strings.HasSuffix(emailDomain, "."+websiteHost) || strings.HasSuffix(websiteHost, "."+emailDomain) {
		return true
	}
	emailTokens := significantDomainTokens(emailDomain)
	websiteTokens := significantDomainTokens(websiteHost)
	if sharesAnyString(emailTokens, websiteTokens) {
		return true
	}
	orgTokens := significantOrganizationTokens(organization)
	return sharesAnyString(emailTokens, orgTokens) && sharesAnyString(websiteTokens, orgTokens)
}

func significantDomainTokens(host string) []string {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	if host == "" {
		return nil
	}
	parts := strings.FieldsFunc(host, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		if part == "www" || part == "mail" || part == "contact" {
			continue
		}
		tokens = appendUniqueStrings(tokens, part)
	}
	return tokens
}

func significantOrganizationTokens(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ",", " ", "(", " ", ")", " ")
	parts := strings.Fields(replacer.Replace(value))
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		tokens = appendUniqueStrings(tokens, part)
	}
	return tokens
}

func sharesAnyString(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
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

func extractDiscoveredContacts(sourceURL, html string) []caseDiscoveredContact {
	evidenceText, roleHint := deriveContactEvidenceContext(sourceURL, html)
	verificationLevel := inferVerificationLevel("website_scrape", roleHint, sourceURL, sourceURL)
	contacts := make([]caseDiscoveredContact, 0, 8)
	for _, email := range extractEmailsFromHTML(html) {
		contacts = appendUniqueDiscoveredContacts(contacts, caseDiscoveredContact{
			Channel:           "email",
			Email:             email,
			SourceURL:         sourceURL,
			EvidenceText:      evidenceText,
			VerificationLevel: verificationLevel,
			RoleHint:          roleHint,
		})
	}
	for _, phone := range extractPhonesFromHTML(html) {
		contacts = appendUniqueDiscoveredContacts(contacts, caseDiscoveredContact{
			Channel:           "phone",
			Phone:             phone,
			SourceURL:         sourceURL,
			EvidenceText:      evidenceText,
			VerificationLevel: verificationLevel,
			RoleHint:          roleHint,
		})
	}
	for _, social := range extractSocialRefsFromHTML(html) {
		contacts = appendUniqueDiscoveredContacts(contacts, caseDiscoveredContact{
			Channel:           "social",
			Social:            social,
			SourceURL:         sourceURL,
			EvidenceText:      evidenceText,
			VerificationLevel: verificationLevel,
			RoleHint:          roleHint,
		})
	}
	return contacts
}

func deriveContactEvidenceContext(sourceURL, html string) (string, string) {
	doc, err := xhtml.Parse(strings.NewReader(html))
	if err != nil {
		roleHint := inferRoleHintFromContext(sourceURL, sourceURL)
		return compactWhitespace(sourceURL), roleHint
	}
	title := compactWhitespace(nodeText(findFirstElement(doc, "title")))
	heading := compactWhitespace(nodeText(findFirstHeading(doc)))
	contextParts := make([]string, 0, 3)
	contextParts = appendUniqueStrings(contextParts, heading, title)
	evidenceText := compactWhitespace(strings.Join(contextParts, " · "))
	if evidenceText == "" {
		evidenceText = compactWhitespace(sourceURL)
	}
	roleHint := inferRoleHintFromContext(sourceURL, evidenceText)
	return evidenceText, roleHint
}

func inferRoleHintFromContext(sourceURL, context string) string {
	corpus := strings.ToLower(strings.Join([]string{sourceURL, context}, " "))
	switch {
	case containsAny(corpus, []string{"hausdienst", "facility", "facilities", "gebäudemanagement", "maintenance", "operations"}):
		return "facility_manager"
	case containsAny(corpus, []string{"schulleitung", "headmaster", "principal", "station manager", "site manager", "leitung"}):
		return "site_leadership"
	case containsAny(corpus, []string{"schulverwaltung", "administration", "office", "sekretariat", "customer service"}):
		return "operator_admin"
	case containsAny(corpus, []string{"hochbau", "bauamt", "building department", "planning", "bau und planung", "baukontrolle", "inspector"}):
		return "building_authority"
	case containsAny(corpus, []string{"feuerpolizei", "fire marshal", "fire safety"}):
		return "fire_authority"
	case containsAny(corpus, []string{"polizei", "public safety", "security office", "emergency management"}):
		return "public_safety"
	case containsAny(corpus, []string{"webmaster", "ict", "communications", "kommunikation"}):
		return "communications"
	default:
		return ""
	}
}

func mergeDiscoveredRoleType(baseRole, hint string) string {
	baseRole = strings.TrimSpace(baseRole)
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return emptyDefault(baseRole, "contact")
	}
	switch baseRole {
	case "", "contact", "operator":
		return hint
	default:
		return baseRole
	}
}

func inferVerificationLevel(source, roleType, websiteURL, sourceURL string) string {
	lowerSource := strings.ToLower(strings.TrimSpace(source))
	lowerRole := strings.ToLower(strings.TrimSpace(roleType))
	switch {
	case lowerSource == "inferred_contact":
		return "inferred"
	case strings.Contains(lowerSource, "area_contact"):
		return "mapped_area_contact"
	case strings.Contains(lowerSource, "google_places"):
		return "directory_listing"
	case strings.Contains(lowerSource, "osm_"):
		return "openstreetmap"
	case lowerRole == "building_authority" || lowerRole == "fire_authority" || lowerRole == "public_safety":
		if isLikelyGovernmentHost(firstNonEmpty(sourceURL, websiteURL)) {
			return "official_authority_page"
		}
		return "authority_reference"
	case strings.Contains(lowerSource, "website"):
		return "official_site_page"
	case strings.Contains(lowerSource, "web_search"):
		return "web_search_result"
	default:
		return "discovered"
	}
}

func appendUniqueDiscoveredContacts(base []caseDiscoveredContact, values ...caseDiscoveredContact) []caseDiscoveredContact {
	seen := make(map[string]struct{}, len(base))
	for _, item := range base {
		if key := discoveredContactKey(item); key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, item := range values {
		if key := discoveredContactKey(item); key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			base = append(base, item)
		}
	}
	return base
}

func discoveredContactKey(item caseDiscoveredContact) string {
	switch item.Channel {
	case "email":
		if item.Email == "" {
			return ""
		}
		return "email:" + strings.ToLower(item.Email)
	case "phone":
		if item.Phone == "" {
			return ""
		}
		return "phone:" + phoneDigits(item.Phone)
	case "social":
		if item.Social.Platform == "" || item.Social.Handle == "" {
			return ""
		}
		return "social:" + strings.ToLower(item.Social.Platform+":"+item.Social.Handle)
	default:
		return ""
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

func findFirstElement(node *xhtml.Node, tag string) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, tag) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirstElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func findFirstHeading(node *xhtml.Node) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode {
		switch strings.ToLower(node.Data) {
		case "h1", "h2", "h3":
			return node
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirstHeading(child); found != nil {
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

func extractObfuscatedEmailsFromHTML(raw string) []string {
	doc, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	emails := []string{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		for _, attr := range node.Attr {
			if !strings.EqualFold(attr.Key, "x-init") {
				continue
			}
			for _, email := range decodeObfuscatedEmailsFromInit(attr.Val) {
				emails = appendUniqueStrings(emails, email)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return emails
}

var caseDiscoveryObfuscatedArrayRegex = regexp.MustCompile(`Array\(([\d,\s]+)\)`)

func decodeObfuscatedEmailsFromInit(raw string) []string {
	matches := caseDiscoveryObfuscatedArrayRegex.FindAllStringSubmatch(raw, -1)
	emails := []string{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		parts := strings.Split(match[1], ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			values = append(values, trimmed)
		}
		sort.Strings(values)
		var b strings.Builder
		for _, value := range values {
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
				b.Reset()
				break
			}
			b.WriteByte(byte(parsed % 256))
		}
		email := normalizeEmail(b.String())
		if email != "" {
			emails = appendUniqueStrings(emails, email)
		}
	}
	return emails
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
	for _, email := range extractObfuscatedEmailsFromHTML(html) {
		emails = appendUniqueStrings(emails, email)
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
			if normalized := normalizePhone(match[1]); looksLikeDirectDialPhone(match[1], normalized) {
				phones = appendUniqueStrings(phones, normalized)
			}
		}
	}
	for _, line := range visiblePhoneContextLinesFromHTML(html) {
		for _, match := range caseDiscoveryPhoneRegex.FindAllString(line, -1) {
			if normalized := normalizePhone(match); looksLikePublishedPhone(match, normalized, line) {
				phones = appendUniqueStrings(phones, normalized)
			}
		}
	}
	out := make([]string, 0, len(phones))
	for _, phone := range phones {
		if phone != "" {
			out = append(out, phone)
		}
	}
	return out
}

func looksLikeDirectDialPhone(raw, normalized string) bool {
	return looksLikePublishedPhone(raw, normalized, "tel")
}

func looksLikePublishedPhone(raw, normalized string, contextParts ...string) bool {
	raw = strings.TrimSpace(raw)
	if normalized == "" {
		return false
	}
	digits := phoneDigits(normalized)
	if len(digits) < 7 || len(digits) > 15 {
		return false
	}
	if looksLikeDateOrTimeValue(raw) {
		return false
	}
	context := strings.ToLower(strings.Join(contextParts, " "))
	if strings.HasPrefix(raw, "+") || strings.HasPrefix(normalized, "+") || strings.HasPrefix(raw, "0") || strings.HasPrefix(normalized, "0") {
		return true
	}
	if strings.ContainsAny(raw, "()") {
		return true
	}
	if containsAny(context, caseDiscoveryPhoneContextKeywords) && strings.ContainsAny(raw, " .-/") {
		return true
	}
	return false
}

func visibleTextFromHTML(html string) string {
	root, err := xhtml.Parse(strings.NewReader(html))
	if err != nil {
		return html
	}
	var parts []string
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			name := strings.ToLower(node.Data)
			if name == "script" || name == "style" {
				return
			}
		}
		if node.Type == xhtml.TextNode {
			text := compactWhitespace(node.Data)
			if text != "" {
				parts = append(parts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return strings.Join(parts, " ")
}

func visiblePhoneContextLinesFromHTML(html string) []string {
	root, err := xhtml.Parse(strings.NewReader(html))
	if err != nil {
		return visiblePhoneContextLinesFromFallback(html)
	}
	lines := []string{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			name := strings.ToLower(node.Data)
			if name == "script" || name == "style" {
				return
			}
			if isPhoneContextContainer(name) {
				text := compactWhitespace(nodeText(node))
				if isPhoneContextLine(text) {
					lines = appendUniqueStrings(lines, text)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return lines
}

func visiblePhoneContextLinesFromFallback(html string) []string {
	lines := []string{}
	for _, rawLine := range strings.Split(html, "\n") {
		line := compactWhitespace(rawLine)
		if isPhoneContextLine(line) {
			lines = appendUniqueStrings(lines, line)
		}
	}
	return lines
}

func isPhoneContextContainer(tag string) bool {
	switch tag {
	case "p", "li", "address", "td", "th", "a", "span", "div":
		return true
	default:
		return false
	}
}

func isPhoneContextLine(line string) bool {
	line = compactWhitespace(line)
	if line == "" {
		return false
	}
	if len(line) > 220 {
		return false
	}
	lower := strings.ToLower(line)
	if !containsAny(lower, caseDiscoveryPhoneContextKeywords) {
		return false
	}
	return caseDiscoveryPhoneRegex.MatchString(line)
}

func looksLikeDateOrTimeValue(raw string) bool {
	raw = compactWhitespace(raw)
	if raw == "" {
		return false
	}
	if caseDiscoveryTimeRangeRegex.MatchString(raw) || caseDiscoveryDateRangeRegex.MatchString(raw) {
		return true
	}
	if strings.Contains(raw, ":") {
		return true
	}
	return false
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
	doc, err := xhtml.Parse(strings.NewReader(html))
	if err != nil {
		return extractContactLinksByRegex(base, html)
	}
	links := []string{}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "a" {
			href := strings.TrimSpace(attrValue(node, "href"))
			if href != "" {
				corpus := strings.ToLower(strings.Join([]string{href, nodeText(node)}, " "))
				if containsAny(corpus, []string{
					"contact", "kontakt", "about", "impressum", "support", "directory", "team",
					"facility", "maintenance", "hausdienst", "leitung", "verwaltung", "administration",
					"planning", "hochbau", "bauamt", "bau und planung", "baukontrolle", "fire", "feuerpolizei",
				}) {
					parsed, err := url.Parse(href)
					if err == nil {
						resolved := base.ResolveReference(parsed)
						if sameHost(base, resolved) {
							links = appendUniqueStrings(links, resolved.String())
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if len(links) > 4 {
		links = links[:4]
	}
	return links
}

func extractContactLinksByRegex(base *url.URL, html string) []string {
	links := []string{}
	for _, match := range caseDiscoveryHrefRegex.FindAllStringSubmatch(html, -1) {
		if len(match) < 2 {
			continue
		}
		href := strings.TrimSpace(match[1])
		lower := strings.ToLower(href)
		if !containsAny(lower, []string{
			"contact", "kontakt", "about", "impressum", "support", "directory", "team",
			"facility", "maintenance", "hausdienst", "leitung", "verwaltung", "administration",
			"planning", "hochbau", "bauamt", "baukontrolle", "fire", "feuerpolizei",
		}) {
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
	if len(links) > 4 {
		links = links[:4]
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

func isLikelyOfficialStakeholderResult(rawURL string, query caseStakeholderSearchQuery) bool {
	if rawURL == "" {
		return false
	}
	if query.RoleType == "building_authority" || query.RoleType == "fire_authority" || query.RoleType == "public_safety" {
		return isLikelyGovernmentHost(rawURL) || hostLooksLikeOrganization(rawURL, query.Organization)
	}
	return true
}

func isLikelyGovernmentHost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	parts := strings.Split(host, ".")
	switch {
	case strings.HasSuffix(host, ".gov"),
		strings.Contains(host, ".gov."),
		strings.HasSuffix(host, ".gouv.fr"),
		strings.HasSuffix(host, ".admin.ch"),
		strings.HasSuffix(host, ".gc.ca"),
		strings.HasSuffix(host, ".gv.at"),
		strings.Contains(host, "stadt"),
		strings.Contains(host, "cityof"),
		strings.Contains(host, "municipality"),
		strings.Contains(host, "commune"),
		strings.Contains(host, "adliswil.ch"):
		return true
	case len(parts) == 2 && len(parts[1]) <= 3 && len(parts[0]) >= 3 && !strings.Contains(parts[0], "example"):
		return true
	default:
		return false
	}
}

func hostLooksLikeOrganization(rawURL, organization string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host == "" {
		return false
	}
	candidate := strings.ToLower(organization)
	candidate = strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "-", " ", "_", " ").Replace(candidate)
	for _, token := range strings.Fields(candidate) {
		if len(token) < 4 {
			continue
		}
		if strings.Contains(host, token) {
			return true
		}
	}
	return false
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
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
	if strings.HasPrefix(phone, "00") {
		return "+" + strings.TrimPrefix(digits, "00")
	}
	if !(strings.HasPrefix(phone, "0") || strings.ContainsAny(phone, " ()-./")) {
		return ""
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
