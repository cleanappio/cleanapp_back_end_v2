package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"report-listener/models"
)

func TestCaseTargetMergerPreservesMultichannelTargets(t *testing.T) {
	merger := newCaseTargetMerger(8)
	merger.Add(models.CaseEscalationTarget{
		Organization:    "Town Hall",
		Email:           "info@town.gov",
		TargetSource:    "inferred_contact",
		ConfidenceScore: 0.55,
		Rationale:       "LLM inferred mailbox.",
	})
	merger.Add(models.CaseEscalationTarget{
		Organization:    "Town Hall",
		Channel:         "email",
		Email:           "info@town.gov",
		Website:         "https://town.gov",
		ContactURL:      "https://town.gov/contact",
		TargetSource:    "website_scrape",
		ConfidenceScore: 0.81,
		Rationale:       "Email scraped from official site.",
	})
	merger.Add(models.CaseEscalationTarget{
		Organization:    "Town Hall",
		Channel:         "phone",
		Phone:           "+1 (555) 101-2020",
		TargetSource:    "google_places",
		ConfidenceScore: 0.8,
	})
	merger.Add(models.CaseEscalationTarget{
		Organization:    "Town Hall",
		Channel:         "website",
		Website:         "town.gov",
		ContactURL:      "https://town.gov/contact",
		TargetSource:    "google_places",
		ConfidenceScore: 0.7,
	})
	merger.Add(models.CaseEscalationTarget{
		Organization:    "Town Hall",
		Channel:         "social",
		SocialPlatform:  "x",
		SocialHandle:    "TownWorks",
		ContactURL:      "https://x.com/TownWorks",
		TargetSource:    "website_scrape",
		ConfidenceScore: 0.69,
	})

	targets := merger.Targets()
	if len(targets) != 4 {
		t.Fatalf("expected 4 distinct targets, got %d", len(targets))
	}
	if targets[0].Email != "info@town.gov" || targets[0].ConfidenceScore != 0.81 {
		t.Fatalf("expected merged email target to keep higher-confidence fields, got %#v", targets[0])
	}
	if targets[0].ContactURL != "https://town.gov/contact" {
		t.Fatalf("expected merged email target to keep contact url, got %#v", targets[0])
	}
	if targets[1].Channel != "phone" {
		t.Fatalf("expected phone target ranked second, got %#v", targets[1])
	}
	if targets[2].Channel != "website" && targets[3].Channel != "website" {
		t.Fatalf("expected website target to be preserved, got %#v", targets)
	}
	if targets[2].Channel != "social" && targets[3].Channel != "social" {
		t.Fatalf("expected social target to be preserved, got %#v", targets)
	}
}

func TestExtractPublicContactsFromHTML(t *testing.T) {
	html := `<html><body>
<a href="mailto:facilities@school.edu">Email</a>
<div>Backup: office@school.edu</div>
<a href="tel:+1 (555) 404-1212">Call</a>
<a href="https://x.com/SchoolOps">X</a>
<a href="/contact">Contact</a>
</body></html>`

	emails := extractEmailsFromHTML(html)
	if len(emails) != 2 || emails[0] != "facilities@school.edu" {
		t.Fatalf("unexpected emails: %#v", emails)
	}
	phones := extractPhonesFromHTML(html)
	if len(phones) != 1 || phones[0] != "+15554041212" {
		t.Fatalf("unexpected phones: %#v", phones)
	}
	socials := extractSocialRefsFromHTML(html)
	if len(socials) != 1 || socials[0].Platform != "x" || socials[0].Handle != "SchoolOps" {
		t.Fatalf("unexpected socials: %#v", socials)
	}
	base, _ := url.Parse("https://school.edu")
	links := extractContactLinks(base, html)
	if len(links) != 1 || links[0] != "https://school.edu/contact" {
		t.Fatalf("unexpected contact links: %#v", links)
	}
}

func TestExtractObfuscatedEmailsFromHTML(t *testing.T) {
	html := `<div><a x-init="const a=Array(1245995116,1313994099,1014783091,1224001380,1444455779,1362649449,1053258600,1140438373,1318570615,1301436521,1087471733,1117487980,1180676705,1146447936,1420165934,1464079208,1039002723,1368978540); a.sort(); let str = '';for (i=0; i<a.length; ++i){ str += String.fromCharCode(a[i]%256); }$el.setAttribute('href','mailto:' + str);$el.textContent = str;"></a></div>`

	emails := extractEmailsFromHTML(html)
	if len(emails) != 1 {
		t.Fatalf("expected 1 decoded email, got %#v", emails)
	}
	if emails[0] != "schule@adliswil.ch" {
		t.Fatalf("unexpected decoded email: %#v", emails)
	}
}

func TestExtractPhonesFromVisibleTextOnly(t *testing.T) {
	html := `<div>
<a x-init="const a=Array(1093293870,1210994537,1045222498,1413970540); a.sort();"></a>
<div>Schulverwaltung</div>
<div>Telefon +41 44 711 78 60</div>
</div>`

	phones := extractPhonesFromHTML(html)
	if len(phones) != 1 {
		t.Fatalf("expected 1 visible phone, got %#v", phones)
	}
	if phones[0] != "+41447117860" {
		t.Fatalf("unexpected phone extraction: %#v", phones)
	}
}

func TestExtractPhonesRejectsObfuscationIntegers(t *testing.T) {
	html := `<div><a x-init="const a=Array(1093293870,1210994537,1045222498,1413970540); a.sort();"></a></div>`
	phones := extractPhonesFromHTML(html)
	if len(phones) != 0 {
		t.Fatalf("expected no phones from obfuscation integers, got %#v", phones)
	}
}

func TestExtractPhonesRejectsOfficeHoursRanges(t *testing.T) {
	html := `<div>
<p>Telefon 044 711 77 77</p>
<p>Öffnungszeiten 08.00 - 11.30 / 13.30 - 16.00</p>
</div>`

	phones := extractPhonesFromHTML(html)
	if len(phones) != 1 {
		t.Fatalf("expected only the real phone number, got %#v", phones)
	}
	if phones[0] != "0447117777" {
		t.Fatalf("unexpected phone extraction: %#v", phones)
	}
}

func TestExtractLocalizedContactLinks(t *testing.T) {
	html := `<html><body>
<a href="/kontakt">Kontakt</a>
<a href="/team/hausdienst">Hausdienst</a>
<a href="/bauen/hochbau">Hochbau</a>
</body></html>`

	base, _ := url.Parse("https://city.example")
	links := extractContactLinks(base, html)
	if len(links) != 3 {
		t.Fatalf("expected 3 localized contact links, got %#v", links)
	}
}

func TestSearchGooglePlacesParsesFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/places:searchText" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Goog-Api-Key"); got != "test-key" {
			t.Fatalf("unexpected api key header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"places":[{"displayName":{"text":"Town Hall"},"formattedAddress":"1 Main St","websiteUri":"https://town.gov","nationalPhoneNumber":"+1 555 888 1212","googleMapsUri":"https://maps.google.com/?cid=123"}]}`))
	}))
	defer server.Close()

	discoverer := &caseContactDiscoverer{
		httpClient:          server.Client(),
		googlePlacesAPIKey:  "test-key",
		googlePlacesBaseURL: server.URL,
	}
	places, err := discoverer.searchGooglePlaces(context.Background(), "Town Hall", 47.0, 8.0)
	if err != nil {
		t.Fatalf("searchGooglePlaces returned error: %v", err)
	}
	if len(places) != 1 {
		t.Fatalf("expected 1 place, got %d", len(places))
	}
	if places[0].DisplayName != "Town Hall" || places[0].WebsiteURI != "https://town.gov/" || places[0].NationalPhoneNumber != "+15558881212" {
		t.Fatalf("unexpected place payload: %#v", places[0])
	}
}

func TestEnrichTargetsDropsInferredFallbackWhenActualTargetsExist(t *testing.T) {
	discoverer := &caseContactDiscoverer{}
	targets := discoverer.EnrichTargets(context.Background(), nil, []models.CaseEscalationTarget{
		{
			Organization:    "Schulhaus Kopfholz",
			Channel:         "email",
			Email:           "hausdienst@schule-adliswil.ch",
			TargetSource:    "area_contact",
			ConfidenceScore: 0.9,
		},
		{
			Organization:    "Random Vendor",
			Channel:         "email",
			Email:           "support@buildingmanagement.com",
			TargetSource:    "inferred_contact",
			ConfidenceScore: 0.6,
		},
	}, 8)

	if len(targets) != 1 {
		t.Fatalf("expected inferred fallback to be dropped, got %#v", targets)
	}
	if targets[0].Email != "hausdienst@schule-adliswil.ch" {
		t.Fatalf("unexpected surviving target: %#v", targets[0])
	}
}

func TestNormalizeCaseEscalationTargetRepairsLegacyWebsiteEmail(t *testing.T) {
	target, ok := normalizeCaseEscalationTarget(models.CaseEscalationTarget{
		RoleType:        "contact",
		Organization:    "Schulhaus Kopfholz",
		Email:           "https://www.schule-adliswil.ch",
		TargetSource:    "area_contact",
		ConfidenceScore: 0.9,
	})
	if !ok {
		t.Fatalf("expected legacy website target to normalize")
	}
	if target.Email != "" {
		t.Fatalf("expected legacy website email to be cleared, got %#v", target)
	}
	if target.Channel != "website" {
		t.Fatalf("expected website channel, got %#v", target)
	}
	if target.Website != "https://www.schule-adliswil.ch/" {
		t.Fatalf("unexpected website normalization: %#v", target)
	}
	if target.ContactURL != "https://www.schule-adliswil.ch/" {
		t.Fatalf("unexpected contact url normalization: %#v", target)
	}
}

func TestNormalizeCaseEscalationTargetRejectsBogusPersistedPhone(t *testing.T) {
	target, ok := normalizeCaseEscalationTarget(models.CaseEscalationTarget{
		RoleType:        "contact",
		Organization:    "Schulhaus Kopfholz",
		Channel:         "phone",
		Phone:           "1048513395",
		EvidenceText:    "Herzlich willkommen auf der Webseite der Adliswiler Schulen",
		TargetSource:    "area_contact_website",
		ConfidenceScore: 0.88,
	})
	if ok {
		t.Fatalf("expected bogus persisted phone target to be rejected, got %#v", target)
	}
}

func TestNormalizeCaseEscalationTargetDowngradesEmptyPhoneRowToWebsite(t *testing.T) {
	target, ok := normalizeCaseEscalationTarget(models.CaseEscalationTarget{
		RoleType:        "contact",
		Organization:    "Schulhaus Kopfholz",
		Channel:         "phone",
		Phone:           "1048513395",
		Website:         "https://www.schule-adliswil.ch/",
		ContactURL:      "https://www.schule-adliswil.ch/schule-adliswil/ueberblick/kontakt/p-183677/",
		EvidenceText:    "Herzlich willkommen auf der Webseite der Adliswiler Schulen",
		TargetSource:    "area_contact_website",
		ConfidenceScore: 0.88,
	})
	if !ok {
		t.Fatalf("expected target to survive as website fallback")
	}
	if target.Channel != "website" || target.Phone != "" {
		t.Fatalf("expected bogus phone row to downgrade to website, got %#v", target)
	}
}

func TestBuildCaseStakeholderSearchQueriesPrefersLocationContextName(t *testing.T) {
	queries := buildCaseStakeholderSearchQueries(
		[]string{"Extreme Structural Hazard: Bricks Separating from Primary School Facade"},
		&caseLocationContext{
			PrimaryName: "Schulhaus Kopfholz",
			City:        "Adliswil",
			State:       "Zürich",
			CountryCode: "ch",
		},
		caseHazardProfile{Structural: true, Severe: true},
	)
	if len(queries) == 0 {
		t.Fatalf("expected search queries")
	}
	if queries[0].Organization != "Schulhaus Kopfholz" {
		t.Fatalf("expected place name to drive search organization, got %#v", queries[0])
	}
	if queries[0].Query == "" || queries[0].Query == "\"Extreme Structural Hazard: Bricks Separating from Primary School Facade\" contact" {
		t.Fatalf("expected location-aware query, got %#v", queries[0])
	}
}

func TestParseDuckDuckGoSearchResults(t *testing.T) {
	raw := `
<html><body>
  <div class="result results_links results_links_deep web-result">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fandereggpartner.ch%2Freferenzen%2Fkopfholz">Kopfholz Erweiterung | Anderegg Partner</a>
    </h2>
    <div class="result__extras__url">
      <a class="result__url" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fandereggpartner.ch%2Freferenzen%2Fkopfholz">andereggpartner.ch/referenzen/kopfholz</a>
    </div>
    <a class="result__snippet">Architektur und Ausfuehrung fuer die Schulanlage Kopfholz in Adliswil.</a>
  </div>
</body></html>`

	results := parseDuckDuckGoSearchResults(raw)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %#v", results)
	}
	if results[0].URL != "https://andereggpartner.ch/referenzen/kopfholz" {
		t.Fatalf("unexpected url: %#v", results[0])
	}
	if results[0].Title != "Kopfholz Erweiterung | Anderegg Partner" {
		t.Fatalf("unexpected title: %#v", results[0])
	}
	if results[0].Snippet == "" {
		t.Fatalf("expected snippet to be parsed")
	}
}

func TestBuildCaseStakeholderSearchQueriesAddsAuthorityForSevereStructuralHazards(t *testing.T) {
	queries := buildCaseStakeholderSearchQueries(
		[]string{"Schulhaus Kopfholz"},
		&caseLocationContext{
			PrimaryName: "Schulhaus Kopfholz",
			City:        "Adliswil",
			State:       "Zürich",
			CountryCode: "ch",
		},
		caseHazardProfile{
			Structural:         true,
			Severe:             true,
			Urgent:             true,
			ImmediateDanger:    true,
			SensitiveOccupancy: true,
		},
	)

	roleSet := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		roleSet[query.RoleType] = struct{}{}
	}
	for _, required := range []string{"operator", "facility_manager", "building_authority", "fire_authority", "public_safety", "architect", "contractor", "engineer"} {
		if _, ok := roleSet[required]; !ok {
			t.Fatalf("expected query role %q in %#v", required, queries)
		}
	}
}

func TestBuildCaseStakeholderSearchQueriesPrioritizesAuthorities(t *testing.T) {
	queries := buildCaseStakeholderSearchQueries(
		[]string{"Brooklyn F Line Station"},
		&caseLocationContext{
			PrimaryName: "F Line Station",
			City:        "Brooklyn",
			State:       "New York",
			CountryCode: "us",
		},
		caseHazardProfile{
			Structural:         true,
			Severe:             true,
			Urgent:             true,
			ImmediateDanger:    true,
			SensitiveOccupancy: true,
		},
	)
	if len(queries) < 5 {
		t.Fatalf("expected multiple stakeholder queries, got %#v", queries)
	}
	if queries[0].RoleType != "operator" {
		t.Fatalf("expected operator query first, got %#v", queries[0])
	}
	if queries[1].RoleType != "facility_manager" {
		t.Fatalf("expected facility manager query second, got %#v", queries[1])
	}
	if queries[2].RoleType != "building_authority" {
		t.Fatalf("expected building authority query before project-party queries, got %#v", queries)
	}
}
