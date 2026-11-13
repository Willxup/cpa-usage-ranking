package test

import (
	"testing"

	"cpa-usage-ranking/internal/periods"
)

func TestCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct{ day, ws, we, ms, me string }{
		{"2026-10-31", "2026-10-26", "2026-11-02", "2026-10-01", "2026-11-01"},
		{"2026-11-01", "2026-10-26", "2026-11-02", "2026-11-01", "2026-12-01"},
		{"2026-11-02", "2026-11-02", "2026-11-09", "2026-11-01", "2026-12-01"},
		{"2026-12-31", "2026-12-28", "2027-01-04", "2026-12-01", "2027-01-01"},
		{"2027-01-01", "2026-12-28", "2027-01-04", "2027-01-01", "2027-02-01"},
		{"2024-02-29", "2024-02-26", "2024-03-04", "2024-02-01", "2024-03-01"},
	} {
		b, err := periods.ForDay(tc.day)
		if err != nil || b.Day != tc.day || b.WeekStart != tc.ws || b.WeekEnd != tc.we || b.MonthStart != tc.ms || b.MonthEnd != tc.me {
			t.Errorf("%s: %+v, %v", tc.day, b, err)
		}
	}
	for _, invalid := range []string{"2026-02-29", "2026-1-01", "2026-01-01Z", "9999-12-31", ""} {
		if _, err := periods.ForDay(invalid); err == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
	if periods.AllStart != "0001-01-01" {
		t.Fatal("cumulative key changed")
	}
}
