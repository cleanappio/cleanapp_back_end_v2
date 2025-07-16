package utils

import (
	"strings"
	"testing"
)

func TestGenerateEthereumAddress(t *testing.T) {
	// Generate multiple addresses
	addresses := make(map[string]bool)
	numAddresses := 100

	for i := 0; i < numAddresses; i++ {
		addr, err := GenerateEthereumAddress()
		if err != nil {
			t.Fatalf("GenerateEthereumAddress() error = %v", err)
		}

		// Check format
		if !strings.HasPrefix(addr, "0x") {
			t.Errorf("Address should start with '0x', got: %s", addr)
		}

		// Check length (0x + 40 hex characters)
		if len(addr) != 42 {
			t.Errorf("Address should be 42 characters long, got: %d", len(addr))
		}

		// Check if it's valid hex
		hexPart := addr[2:]
		for _, c := range hexPart {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				t.Errorf("Invalid hex character in address: %c", c)
			}
		}

		// Check uniqueness
		if addresses[addr] {
			t.Errorf("Duplicate address generated: %s", addr)
		}
		addresses[addr] = true
	}
}

func TestHashToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "simple token",
			token: "simple-token-123",
		},
		{
			name:  "JWT-like token",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "unicode token",
			token: "token-with-unicode-世界-🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := HashToken(tt.token)
			hash2 := HashToken(tt.token)

			// Hash should be consistent
			if hash1 != hash2 {
				t.Error("Same token should produce same hash")
			}

			// Hash should be 64 characters (SHA256 in hex)
			if len(hash1) != 64 {
				t.Errorf("Hash should be 64 characters long, got: %d", len(hash1))
			}

			// Hash should be valid hex
			for _, c := range hash1 {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("Invalid hex character in hash: %c", c)
				}
			}
		})
	}

	// Different tokens should produce different hashes
	hash1 := HashToken("token1")
	hash2 := HashToken("token2")
	if hash1 == hash2 {
		t.Error("Different tokens should produce different hashes")
	}
}

func TestNormalizeBrandName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Coca-Cola", "cocacola"},
		{"Coca Cola", "cocacola"},
		{"Coca-Cola & Co.", "cocacolaco"},
		{"Red Bull", "redbull"},
		{"Nike", "nike"},
		{"Adidas.", "adidas"},
		{"Apple Inc.", "appleinc"},
		{"McDonald's", "mcdonalds"},
		{"Starbucks", "starbucks"},
		{"Pepsi, Inc.", "pepsiinc"},
		{"Samsung Electronics", "samsungelectronics"},
		{"Microsoft Corporation", "microsoftcorporation"},
		{"  Coca   Cola  ", "cocacola"},
		{"Coca-Cola and Company", "cocacolacompany"},
	}

	for _, tt := range tests {
		result := NormalizeBrandName(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeBrandName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
