package handlers

import (
	"strings"
	"testing"
)

func TestParseSortExclusions(t *testing.T) {
	for _, raw := range []string{"0", "-1", "abc", "1,,2", strings.Repeat("1,", 128) + "2"} {
		if _, err := parseSortExclusions(raw); err == nil {
			t.Errorf("accepted invalid exclusion list %q", raw)
		}
	}
	got, err := parseSortExclusions("12, 34,56")
	if err != nil || len(got) != 3 || got[0] != 12 || got[1] != 34 || got[2] != 56 {
		t.Fatalf("unexpected exclusions: %v %v", got, err)
	}
	if got, err := parseSortExclusions(""); err != nil || len(got) != 0 {
		t.Fatalf("empty exclusions: %v %v", got, err)
	}
}
