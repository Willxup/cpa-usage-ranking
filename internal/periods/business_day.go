package periods

import (
	"time"
	_ "time/tzdata" // Keep the fixed business timezone available in minimal images.
)

const BusinessTimezone = "Asia/Shanghai"

var businessLocation = func() *time.Location {
	location, err := time.LoadLocation(BusinessTimezone)
	if err != nil {
		panic(err)
	}
	return location
}()

// StartOfDay returns Shanghai midnight for the business day containing now.
// Timestamps stored or transmitted by callers remain UTC instants.
func StartOfDay(now time.Time) time.Time {
	n := now.In(businessLocation)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, businessLocation)
}

func BusinessDay(now time.Time) string {
	return now.In(businessLocation).Format(dayLayout)
}

func NextMidnight(now time.Time) time.Time {
	return StartOfDay(now).AddDate(0, 0, 1)
}

// ParseBusinessDay interprets a calendar date as Shanghai midnight, not UTC.
func ParseBusinessDay(day string) (time.Time, error) {
	parsed, err := time.ParseInLocation(dayLayout, day, businessLocation)
	if err != nil || len(day) != len(dayLayout) || parsed.Format(dayLayout) != day {
		return time.Time{}, ErrInvalidDay
	}
	return parsed, nil
}
