package handlers

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClampStrPreservesUTF8AtByteBoundary(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{"ASCII limit", "abcdef", 4, "abcd"},
		{"existing whitespace trimming", "  café  ", 5, "café"},
		{"exact byte limit", "café", 5, "café"},
		{"accent split", "café", 4, "caf"},
		{"Cyrillic split", "абв", 5, "аб"},
		{"CJK split", "東京", 5, "東"},
		{"emoji split", "A🙂B", 4, "A"},
		{"complete emoji preserved", "A🙂B", 5, "A🙂"},
		{"first character exceeds budget", "東", 2, ""},
		{"zero remains unlimited", "  東京🙂  ", 0, "東京🙂"},
		{"negative remains unlimited", "  café  ", -1, "café"},
		{"empty input", "  ", 255, ""},
		// Matches the incident's shape: 260 valid UTF-8 bytes, 258
		// characters, with a three-byte character spanning byte 255.
		{"report projection at 255 bytes", strings.Repeat("a", 254) + "東xyz", 255, strings.Repeat("a", 254)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampStr(tt.text, tt.max)
			if got != tt.want {
				t.Fatalf("clampStr() = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("clampStr() returned invalid UTF-8")
			}
			if tt.max > 0 && len(got) > tt.max {
				t.Fatalf("clampStr() returned %d bytes, budget is %d", len(got), tt.max)
			}
		})
	}
}

func TestClampStrAllMultilingualCutoffs(t *testing.T) {
	text := "Brussels café Ελληνικά 日本語 русский العربية 🙂"
	for max := 1; max <= len(text); max++ {
		got := clampStr(text, max)
		if !utf8.ValidString(got) || len(got) > max || !strings.HasPrefix(text, got) {
			t.Fatalf("invalid prefix at byte budget %d: %q", max, got)
		}
		// The result must use the largest complete prefix that fits.
		if len(got) < len(text) {
			_, nextSize := utf8.DecodeRuneInString(text[len(got):])
			if len(got)+nextSize <= max {
				t.Fatalf("budget %d omitted a complete character that fits", max)
			}
		}
	}
}

func TestWireDescriptionRetainsFullMaterialBeyondSQLProjection(t *testing.T) {
	description := strings.Repeat("a", 254) + "東xyz🙂"
	sub := cleanAppWireSubmission{SourceID: "fixture-source"}
	sub.Report.Description = description
	normalized, _ := normalizeCleanAppWireSubmission(sub)
	if normalized.Report.Description != description {
		t.Fatal("Wire normalization changed the full report description")
	}
	if got := clampStr(normalized.Report.Description, 255); got != strings.Repeat("a", 254) {
		t.Fatal("SQL projection did not retain the complete UTF-8 prefix")
	}
	firstHash, err := cleanAppWireMaterialHash(normalized)
	if err != nil {
		t.Fatal(err)
	}
	normalized.Report.Description += " additional evidence"
	secondHash, err := cleanAppWireMaterialHash(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("idempotency material ignored description evidence beyond the SQL projection")
	}
}
