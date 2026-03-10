package publicid

import (
	"strings"
	"testing"
)

func TestNewReportID(t *testing.T) {
	first, err := NewReportID()
	if err != nil {
		t.Fatalf("NewReportID() error = %v", err)
	}
	second, err := NewReportID()
	if err != nil {
		t.Fatalf("NewReportID() second error = %v", err)
	}

	if len(first) == 0 || len(first) > MaxLength {
		t.Fatalf("unexpected id length: %q", first)
	}
	if len(second) == 0 || len(second) > MaxLength {
		t.Fatalf("unexpected second id length: %q", second)
	}
	if !strings.HasPrefix(first, ReportPrefix) {
		t.Fatalf("missing prefix in id %q", first)
	}
	if !strings.HasPrefix(second, ReportPrefix) {
		t.Fatalf("missing prefix in id %q", second)
	}
	if first == second {
		t.Fatalf("expected distinct ids, got %q twice", first)
	}
	if !IsReportID(first) {
		t.Fatalf("expected generated id %q to validate", first)
	}
}

func TestIsReportIDRejectsInvalidValues(t *testing.T) {
	cases := []string{
		"",
		"rpt_short",
		"abc_7GkQm9w2NfHc8Lp3XzRtYb",
		"rpt_7GkQm9w2NfHc8Lp3XzRtY*",
	}
	for _, tc := range cases {
		if IsReportID(tc) {
			t.Fatalf("expected %q to be rejected", tc)
		}
	}
}
