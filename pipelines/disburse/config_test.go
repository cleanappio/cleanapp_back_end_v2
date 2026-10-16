package disburse

import (
	"flag"
	"testing"
)

func TestDisburserCredentialsEnvironmentAndLegacyCLI(t *testing.T) {
	oldURL, oldKey := *ethNetworkUrl, *privateKey
	defer func() { *ethNetworkUrl, *privateKey = oldURL, oldKey }()
	t.Setenv("ETH_NETWORK_URL", "environment-provider")
	t.Setenv("KITN_PRIVATE_KEY", "environment-key")
	*ethNetworkUrl, *privateKey = "", ""
	url, key := disburserCredentials()
	if url != "environment-provider" || key != "environment-key" {
		t.Fatal("environment fallback unavailable")
	}
	*ethNetworkUrl, *privateKey = "explicit-provider", "explicit-key"
	url, key = disburserCredentials()
	if url != "explicit-provider" || key != "explicit-key" {
		t.Fatal("legacy explicit flags no longer take precedence")
	}
	// Secrets remain out of the flag defaults, including usage/help output.
	if flag.Lookup("eth_network_url").DefValue != "" || flag.Lookup("eth_private_key").DefValue != "" {
		t.Fatal("legacy flags missing")
	}
}
