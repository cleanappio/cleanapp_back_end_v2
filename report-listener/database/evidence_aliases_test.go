package database

import (
	"reflect"
	"strings"
	"testing"
)

// These exact identities also appear in the sender's verified company routing
// registry. Listing the expected sets independently catches an omitted alias.
func expectedEvidenceCompanyAliases() map[string][]string {
	return map[string][]string{
		"anthropic": {"anthropic", "anthropic.com", "claude", "claudeai", "claude.ai", "claude ai", "@claudeai", "claudecode", "claude-code", "claude_code", "claude code"},
		"openai":    {"openai", "openai.com", "chatgpt", "chat-gpt", "chat gpt", "codex", "openaiapi", "openai-api", "openai_api", "openai api", "openainode", "openai-node", "openai_node", "openai node", "openaipython", "openai-python", "openai_python", "openai python", "whisper", "dall-e", "dalle"},
		"xai":       {"xai", "x.ai", "x-ai", "x_ai", "x ai", "grok", "grok.com"},
	}
}

func TestEvidenceCompanyAliasesCoverVerifiedRoutesWithoutSubstringMatching(t *testing.T) {
	for company, aliases := range expectedEvidenceCompanyAliases() {
		f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: company})
		if err != nil || !reflect.DeepEqual(f.Brands, aliases) {
			t.Fatalf("%s alias scope differs: %v, %v", company, f.Brands, err)
		}
		query, args := evidenceBrandCTE(f)
		if strings.Contains(query, "LIKE") || !strings.Contains(query, "ra.brand_name IN (") {
			t.Fatal("company identity must use exact indexed values")
		}
		if !reflect.DeepEqual(args[1:len(aliases)+1], stringInterfaces(aliases)) {
			t.Fatal("company alias query arguments differ")
		}
	}
	for _, unrelated := range []string{"x", "twitter", "x.com", "notopenai", "grok-news", "anthropic-partner"} {
		f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: unrelated})
		if err != nil || !reflect.DeepEqual(f.Brands, []string{unrelated}) {
			t.Fatalf("unrelated identity was expanded: %s %v %v", unrelated, f.Brands, err)
		}
	}
}

func TestEvidenceExpandedProductAliasesDoNotIncludeSiblingProducts(t *testing.T) {
	for _, tc := range []struct {
		company string
		aliases []string
	}{
		{"anthropic", []string{"anthropic", "anthropic.com"}},
		{"anthropic", []string{"claude", "claudeai", "claude.ai", "claude ai", "@claudeai"}},
		{"anthropic", []string{"claude-code", "claudecode", "claude_code", "claude code"}},
		{"openai", []string{"openai", "openai.com"}},
		{"openai", []string{"openai-api", "openaiapi", "openai_api", "openai api"}},
		{"openai", []string{"openai-node", "openainode", "openai_node", "openai node"}},
		{"openai", []string{"openai-python", "openaipython", "openai_python", "openai python"}},
		{"openai", []string{"chatgpt", "chat-gpt", "chat gpt"}},
		{"openai", []string{"dall-e", "dalle"}},
		{"xai", []string{"xai", "x.ai", "x-ai", "x_ai", "x ai"}},
		{"xai", []string{"grok", "grok.com"}},
	} {
		for _, alias := range tc.aliases {
			f, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: tc.company, Product: alias})
			if err != nil || !reflect.DeepEqual(f.Brands, tc.aliases) || f.Scope().IncludesProducts {
				t.Fatalf("%s product %s broadened: %v %v", tc.company, alias, f.Brands, err)
			}
		}
	}
	for _, product := range []string{"x", "twitter", "x.com", "openai"} {
		if _, err := NormalizeEvidenceFilter(EvidenceFilter{BrandName: "xai", Product: product}); err == nil {
			t.Fatalf("xAI admitted unrelated product %s", product)
		}
	}
}

func stringInterfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}
