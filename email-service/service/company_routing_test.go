package service

import (
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCompanyRoutingAliasesShareOfficialRouteAndThrottle(t *testing.T) {
	groups := map[string][]string{
		"anthropic": {"Anthropic", "anthropic.com", "Claude", "Claude AI", "claude.ai", "Claude Code", "claude-code", "claudecode"},
		"openai":    {"OpenAI", "openai.com", "ChatGPT", "chat-gpt", "Codex", "OpenAI API", "openai-api", "openai-node", "openai_python", "Whisper", "DALL-E", "dalle"},
		"xai":       {"xAI", "x.ai", "x-ai", "Grok", "grok.com", "  GROK  "},
	}
	for company, aliases := range groups {
		for _, alias := range aliases {
			t.Run(alias, func(t *testing.T) {
				route, known := ResolveCompanyRoute(alias)
				if !known || route.CompanyKey != company || route.ThrottleKey != company || route.DisplayName == "" {
					t.Fatalf("wrong company or shared throttle for %q: %#v known=%v", alias, route, known)
				}
				if len(route.Recipients) != 1 || !strings.HasPrefix(route.Recipients[0].Email, "support@") {
					t.Fatalf("expected the single curated support route: %#v", route.Recipients)
				}
			})
		}
	}
}

func TestCompanyRoutingDoesNotConfuseXOrGuessFromTextOrDomains(t *testing.T) {
	for _, alias := range []string{"", "X", "Twitter", "x.com", "twitter.com", "X/Twitter", "xAI / X", "Grok on X", "notgrok", "grokcorp", "Claude at another company", "OpenAI competitor", "anthropic.evil.com", "support@x.ai", "https://x.ai", "https://openai.com", "c.l.a.u.d.e"} {
		if route, known := ResolveCompanyRoute(alias); known || len(route.Recipients) != 0 || route.CompanyKey != "" || route.ThrottleKey != "" {
			t.Fatalf("unverified alias %q received a route: %#v", alias, route)
		}
	}
}

func TestCompanyRoutingRecipientsCarryOfficialSourceAndReviewDate(t *testing.T) {
	expected := map[string]string{"anthropic": "support@anthropic.com", "openai": "support@openai.com", "xai": "support@x.ai"}
	for company, email := range expected {
		route, _ := ResolveCompanyRoute(company)
		recipient := route.Recipients[0]
		if recipient.Email != email {
			t.Fatalf("unexpected recipient for %s: %s", company, recipient.Email)
		}
		if _, err := mail.ParseAddress(recipient.Email); err != nil {
			t.Fatalf("invalid curated recipient: %v", err)
		}
		source, err := url.Parse(recipient.SourceURL)
		if err != nil || source.Scheme != "https" || source.User != nil || source.Host == "" {
			t.Fatalf("missing official publication source: %q", recipient.SourceURL)
		}
		if _, err := time.Parse("2006-01-02", recipient.ReviewedDate); err != nil {
			t.Fatalf("missing review date: %q", recipient.ReviewedDate)
		}
	}
}

func TestCompanyRoutingReturnedRecipientsCannotModifyLaterRoutes(t *testing.T) {
	route, _ := ResolveCompanyRoute("claude")
	route.Recipients[0].Email = "untrusted@example.invalid"
	route.Recipients = append(route.Recipients, VerifiedCompanyRecipient{Email: "another@example.invalid"})
	route.Aliases[0] = "untrusted"
	next, _ := ResolveCompanyRoute("anthropic")
	if len(next.Recipients) != 1 || next.Recipients[0].Email != "support@anthropic.com" {
		t.Fatalf("a caller modified global routing: %#v", next)
	}
	if next.Aliases[0] != "anthropic" {
		t.Fatalf("a caller modified global aliases: %#v", next.Aliases)
	}
}

func TestCompanyRoutingRegistryHasExactUnambiguousAliasesAndCopies(t *testing.T) {
	routes := VerifiedCompanyRoutes()
	if len(routes) != 3 {
		t.Fatalf("unexpected registry size: %d", len(routes))
	}
	seen := map[string]string{}
	for _, route := range routes {
		for _, alias := range route.Aliases {
			if alias != strings.ToLower(strings.TrimSpace(alias)) {
				t.Fatalf("alias not ready for canonical SQL matching: %q", alias)
			}
			if previous, exists := seen[alias]; exists {
				t.Fatalf("alias %q belongs to both %s and %s", alias, previous, route.CompanyKey)
			}
			seen[alias] = route.CompanyKey
			resolved, known := ResolveCompanyRoute(alias)
			if !known || resolved.CompanyKey != route.CompanyKey {
				t.Fatalf("registry and resolver disagree for %q", alias)
			}
		}
	}
	routes[0].Aliases[0] = "untrusted"
	routes[0].Recipients[0].Email = "untrusted@example.invalid"
	next := VerifiedCompanyRoutes()
	if next[0].Aliases[0] != "anthropic" || next[0].Recipients[0].Email != "support@anthropic.com" {
		t.Fatal("registry enumeration returned mutable global slices")
	}
}
