package test

import (
	"testing"
	"time"

	"cpa-usage-ranking/internal/periods"
)

func TestLeaderboardPreviousCompletePeriods(t *testing.T) {
	for _, tc := range []struct{ now, weekStart, weekEnd, monthStart, monthEnd string }{
		{"2027-01-04T00:00:00Z", "2026-12-28", "2027-01-04", "2026-12-01", "2027-01-01"},
		{"2027-01-03T23:59:59Z", "2026-12-21", "2026-12-28", "2026-12-01", "2027-01-01"},
		{"2024-03-01T12:00:00Z", "2024-02-19", "2024-02-26", "2024-02-01", "2024-03-01"},
		{"2024-02-29T23:59:59Z", "2024-02-19", "2024-02-26", "2024-01-01", "2024-02-01"},
		{"2027-01-03T16:00:00-08:00", "2026-12-28", "2027-01-04", "2026-12-01", "2027-01-01"},
	} {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatal(err)
		}
		week, err := periods.ForDay(periods.LeaderboardDay(periods.Week, now))
		if err != nil || week.WeekStart != tc.weekStart || week.WeekEnd != tc.weekEnd {
			t.Errorf("%s week: %+v %v", tc.now, week, err)
		}
		month, err := periods.ForDay(periods.LeaderboardDay(periods.Month, now))
		if err != nil || month.MonthStart != tc.monthStart || month.MonthEnd != tc.monthEnd {
			t.Errorf("%s month: %+v %v", tc.now, month, err)
		}
		if got := periods.LeaderboardDay(periods.Day, now); got != now.UTC().Format(time.DateOnly) {
			t.Errorf("%s day: %s", tc.now, got)
		}
	}
}

func TestUTCBoundaries(t *testing.T) {
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
