package test

import (
	"testing"
	"time"

	"cpa-usage-ranking/internal/periods"
)

func TestShanghaiBusinessDayIndependentOfInputAndSystemTimezone(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	for _, system := range []*time.Location{time.UTC, time.FixedZone("west", -7*3600), time.FixedZone("east", 12*3600)} {
		time.Local = system
		for _, tc := range []struct{ instant, day, next, week, month string }{
			{"2026-11-15T15:59:59Z", "2026-11-15", "2026-11-15T16:00:00Z", "2026-11-09", "2026-11-01"},
			{"2026-11-15T16:00:00Z", "2026-11-16", "2026-11-16T16:00:00Z", "2026-11-16", "2026-11-01"},
			{"2026-11-30T16:00:00Z", "2026-12-01", "2026-12-01T16:00:00Z", "2026-11-30", "2026-12-01"},
			{"2026-12-31T15:59:59Z", "2026-12-31", "2026-12-31T16:00:00Z", "2026-12-28", "2026-12-01"},
			{"2026-12-31T16:00:00Z", "2027-01-01", "2027-01-01T16:00:00Z", "2026-12-28", "2027-01-01"},
		} {
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []*time.Location{time.UTC, system, time.FixedZone("input", 3*3600)} {
				now = now.In(input)
				if got := periods.BusinessDay(now); got != tc.day {
					t.Fatalf("%s: day %s", tc.instant, got)
				}
				if got := periods.NextMidnight(now).UTC().Format(time.RFC3339); got != tc.next {
					t.Fatalf("%s: next %s", tc.instant, got)
				}
				start, err := periods.ParseBusinessDay(tc.day)
				if err != nil || !start.Equal(periods.StartOfDay(now)) || start.Location().String() != periods.BusinessTimezone {
					t.Fatalf("start %v: %v", start, err)
				}
				bounds, err := periods.ForDay(periods.BusinessDay(now))
				if err != nil || bounds.WeekStart != tc.week || bounds.MonthStart != tc.month {
					t.Fatalf("bounds %+v: %v", bounds, err)
				}
			}
		}
	}
}

func TestParseBusinessDayRejectsInvalidDates(t *testing.T) {
	for _, day := range []string{"", "2026-2-01", "2026-02-29", "2026-12-31T16:00:00Z"} {
		if _, err := periods.ParseBusinessDay(day); err == nil {
			t.Fatalf("accepted %q", day)
		}
	}
}
