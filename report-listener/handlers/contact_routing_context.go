package handlers

import (
	"math"
	"net/url"
	"strings"
)

func inferAssetClass(primaryName string, candidateNames []string, locCtx *caseLocationContext, extraText ...string) string {
	parts := []string{primaryName}
	for _, candidate := range candidateNames {
		parts = append(parts, candidate)
	}
	if locCtx != nil {
		parts = append(parts,
			locCtx.PrimaryName,
			locCtx.ParentOrg,
			locCtx.Operator,
			locCtx.LocationType,
			locCtx.FormattedName,
			locCtx.City,
			locCtx.State,
			locCtx.Country,
		)
	}
	parts = append(parts, extraText...)
	return inferAssetClassFromText(strings.ToLower(strings.Join(parts, " ")))
}

func inferAssetClassFromText(corpus string) string {
	switch {
	case containsAny(corpus, []string{"metro", "subway", "station", "platform", "rail", "railway", "tram", "terminal", "bus depot", "transit"}):
		return "transit_station"
	case containsAny(corpus, []string{"bridge", "overpass", "underpass", "viaduct"}):
		return "bridge"
	case containsAny(corpus, []string{"road", "street", "highway", "freeway", "motorway", "intersection", "sidewalk", "carriageway"}):
		return "roadway"
	case containsAny(corpus, []string{"school", "campus", "kindergarten", "nursery", "daycare", "college", "university"}):
		return "school"
	case containsAny(corpus, []string{"hospital", "clinic", "medical center", "care home", "emergency room"}):
		return "hospital"
	case containsAny(corpus, []string{"mall", "shopping", "supermarket", "store", "retail", "walmart", "ikea", "costco", "target"}):
		return "retail_site"
	case containsAny(corpus, []string{"city hall", "courthouse", "library", "museum", "stadium", "arena", "airport", "civic center", "municipal"}):
		return "public_building"
	case containsAny(corpus, []string{"factory", "warehouse", "plant", "industrial", "depot"}):
		return "industrial_site"
	case containsAny(corpus, []string{"apartment", "residential", "condo", "housing", "tower"}):
		return "residential_site"
	default:
		return "general_site"
	}
}

func deriveJurisdictionHint(locCtx *caseLocationContext) string {
	if locCtx == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(locCtx.City),
		strings.TrimSpace(locCtx.State),
		strings.TrimSpace(locCtx.Country),
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, ", ")
}

func deriveSearchSiteKeywords(primaryName string, candidateNames []string, locCtx *caseLocationContext) []string {
	keywords := []string{}
	for _, candidate := range append([]string{primaryName}, candidateNames...) {
		for _, token := range significantOrganizationTokens(candidate) {
			keywords = appendUniqueStrings(keywords, token)
		}
	}
	if locCtx != nil {
		for _, token := range significantOrganizationTokens(locCtx.PrimaryName) {
			keywords = appendUniqueStrings(keywords, token)
		}
		for _, token := range significantOrganizationTokens(locCtx.ParentOrg) {
			keywords = appendUniqueStrings(keywords, token)
		}
		for _, token := range significantOrganizationTokens(locCtx.Operator) {
			keywords = appendUniqueStrings(keywords, token)
		}
	}
	return keywords
}

func deriveOfficialHostHints(locCtx *caseLocationContext) []string {
	hints := []string{}
	if locCtx == nil {
		return hints
	}
	if host := hostForHint(locCtx.Website); host != "" {
		hints = appendUniqueStrings(hints, host)
	}
	if email := normalizeEmail(locCtx.ContactEmail); email != "" {
		if at := strings.LastIndex(email, "@"); at >= 0 && at < len(email)-1 {
			hints = appendUniqueStrings(hints, strings.ToLower(email[at+1:]))
		}
	}
	return hints
}

func hostForHint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
}

func hostMatchesAnyHint(rawURL string, hints []string) bool {
	host := hostForHint(rawURL)
	if host == "" {
		return false
	}
	for _, hint := range hints {
		hint = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(hint, "www.")))
		if hint == "" {
			continue
		}
		if host == hint || strings.HasSuffix(host, "."+hint) || strings.HasSuffix(hint, "."+host) {
			return true
		}
	}
	return false
}

func tokenMatchCount(corpus string, tokens []string) int {
	count := 0
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if strings.Contains(corpus, token) {
			count++
		}
	}
	return count
}

func searchResultConfidenceBoost(result caseWebSearchResult, query caseStakeholderSearchQuery) float64 {
	corpus := strings.ToLower(strings.Join([]string{result.Title, result.Snippet, result.DisplayURL, result.URL}, " "))
	boost := 0.0
	if hostMatchesAnyHint(result.URL, query.OfficialHostHints) {
		boost += 0.12
	}
	if queryNeedsAuthorityHost(query.RoleType) && isLikelyGovernmentHost(result.URL) {
		boost += 0.08
	}
	if query.JurisdictionHint != "" {
		boost += math.Min(0.05, float64(tokenMatchCount(corpus, significantOrganizationTokens(query.JurisdictionHint)))*0.02)
	}
	boost += math.Min(0.06, float64(tokenMatchCount(corpus, query.SiteKeywords))*0.02)
	for _, keyword := range authorityRoleKeywords(query.RoleType, query.AssetClass) {
		if strings.Contains(corpus, keyword) {
			boost += 0.015
			break
		}
	}
	return math.Min(0.18, boost)
}

func authorityRoleKeywords(roleType, assetClass string) []string {
	roleType = strings.ToLower(strings.TrimSpace(roleType))
	switch roleType {
	case "building_authority":
		return []string{"building department", "building safety", "building inspection", "planning", "hochbau", "bauamt", "bau und planung", "permits"}
	case "fire_authority":
		return []string{"fire marshal", "fire safety", "feuerpolizei", "feuerwehr", "fire prevention"}
	case "public_safety":
		return []string{"public safety", "police", "emergency management", "sicherheit"}
	case "transit_authority":
		return []string{"transit authority", "transport authority", "metro", "subway", "rail", "station management", "verkehrsbetriebe"}
	case "transit_safety":
		return []string{"rail safety", "transit safety", "station safety", "bahn sicherheit", "transport safety"}
	case "public_works":
		return []string{"public works", "roads", "highways", "tiefbau", "works department", "maintenance"}
	case "traffic_authority":
		return []string{"traffic engineering", "transportation department", "verkehrsamt", "verkehrsplanung"}
	case "infrastructure_authority":
		if assetClass == "bridge" {
			return []string{"bridge maintenance", "bridge authority", "infrastructure", "highway maintenance", "bruecken"}
		}
		return []string{"infrastructure", "maintenance authority", "transportation infrastructure", "infrastruktur"}
	default:
		return nil
	}
}

func queryNeedsAuthorityHost(roleType string) bool {
	switch strings.ToLower(strings.TrimSpace(roleType)) {
	case "building_authority", "fire_authority", "public_safety", "transit_authority", "transit_safety", "public_works", "traffic_authority", "infrastructure_authority":
		return true
	default:
		return false
	}
}
