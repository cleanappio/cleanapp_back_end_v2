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
