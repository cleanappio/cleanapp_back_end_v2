package service

import (
	"strings"
)

// VerifiedCompanyRecipient is a contact route checked against the cited company
// source. It does not assert inbox delivery, current staffing, or a response.
type VerifiedCompanyRecipient struct {
	Email        string
	SourceURL    string
	ReviewedDate string
}

type CompanyRoute struct {
	CompanyKey  string
	DisplayName string
	ThrottleKey string
	Aliases     []string
	Recipients  []VerifiedCompanyRecipient
}

// This small registry is deliberately curated. A brand label, matching domain,
// user-provided address, or model-generated alias does not verify a recipient.
// Refresh the cited source and review date when changing a contact route.
var officialCompanyRoutes = map[string]CompanyRoute{
	"anthropic": {
		CompanyKey: "anthropic", DisplayName: "Anthropic", ThrottleKey: "anthropic",
		Aliases: []string{"anthropic", "anthropic.com", "claude", "claudeai", "claude.ai", "claude ai", "@claudeai", "claudecode", "claude-code", "claude_code", "claude code"},
		Recipients: []VerifiedCompanyRecipient{{
			Email: "support@anthropic.com", SourceURL: "https://platform.claude.com/docs/en/about-claude/pricing", ReviewedDate: "2026-10-15",
		}},
	},
	"openai": {
		CompanyKey: "openai", DisplayName: "OpenAI", ThrottleKey: "openai",
		Aliases: []string{"openai", "openai.com", "chatgpt", "chat-gpt", "chat gpt", "codex", "openaiapi", "openai-api", "openai_api", "openai api", "openainode", "openai-node", "openai_node", "openai node", "openaipython", "openai-python", "openai_python", "openai python", "whisper", "dall-e", "dalle"},
		Recipients: []VerifiedCompanyRecipient{{
			Email: "support@openai.com", SourceURL: "https://status.openai.com/incidents/01KSJZHDY7E780KF9J2ZZ8M0GE", ReviewedDate: "2026-10-15",
		}},
	},
	"xai": {
		CompanyKey: "xai", DisplayName: "xAI", ThrottleKey: "xai",
		Aliases: []string{"xai", "x.ai", "x-ai", "x_ai", "x ai", "grok", "grok.com"},
		Recipients: []VerifiedCompanyRecipient{{
			Email: "support@x.ai", SourceURL: "https://x.ai/contact", ReviewedDate: "2026-10-15",
		}},
	},
}

func copyCompanyRoute(route CompanyRoute) CompanyRoute {
	route.Recipients = append([]VerifiedCompanyRecipient(nil), route.Recipients...)
	route.Aliases = append([]string(nil), route.Aliases...)
	return route
}

// VerifiedCompanyRoutes returns the complete, deterministic registry. Aliases
// are the same exact lower-case values recognized by ResolveCompanyRoute and
// can exclude these cohorts from generic sending or join prior throttle keys.
func VerifiedCompanyRoutes() []CompanyRoute {
	routes := make([]CompanyRoute, 0, len(officialCompanyRoutes))
	for _, key := range []string{"anthropic", "openai", "xai"} {
		routes = append(routes, copyCompanyRoute(officialCompanyRoutes[key]))
	}
	return routes
}

// ResolveCompanyRoute accepts exact known company/product aliases only. X,
// Twitter and x.com remain separate from xAI/Grok; report text is never searched
// for a company substring. Unknown brands do not receive a guessed route.
// Callers must replace inferred recipients for a known route, not merge them.
func ResolveCompanyRoute(brandName string) (CompanyRoute, bool) {
	alias := strings.ToLower(strings.TrimSpace(brandName))
	for _, route := range officialCompanyRoutes {
		for _, knownAlias := range route.Aliases {
			if alias == knownAlias {
				return copyCompanyRoute(route), true
			}
		}
	}
	return CompanyRoute{}, false
}
