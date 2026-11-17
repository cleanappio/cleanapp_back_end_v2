package database

import (
	"testing"
	"time"
)

func TestAddWorkingDaysSkipsWeekends(t *testing.T) {
	day := func(s string) time.Time {
		parsed, err := time.Parse("2006-01-02 15:04", s)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, tc := range []struct {
		from string
		n    int
		want string
	}{
		{"2026-11-16 09:30", 5, "2026-11-23 09:30"},  // Monday + 5 = next Monday
		{"2026-11-18 09:30", 5, "2026-11-25 09:30"},  // Wednesday + 5 crosses one weekend
		{"2026-11-20 09:30", 1, "2026-11-23 09:30"},  // Friday + 1 = Monday
		{"2026-11-21 09:30", 1, "2026-11-23 09:30"},  // Saturday + 1 = Monday
		{"2026-11-23 09:30", -5, "2026-11-16 09:30"}, // and back
		{"2026-11-23 09:30", -1, "2026-11-20 09:30"}, // Monday - 1 = Friday
		{"2026-11-22 09:30", -1, "2026-11-20 09:30"}, // Sunday - 1 = Friday
		{"2026-11-18 09:30", 0, "2026-11-18 09:30"},
	} {
		if got := AddWorkingDays(day(tc.from), tc.n); !got.Equal(day(tc.want)) {
			t.Errorf("AddWorkingDays(%s, %d) = %s, want %s", tc.from, tc.n, got.Format("2006-01-02 15:04"), tc.want)
		}
	}
}
