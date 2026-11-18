// Package periods 定义日结使用的 UTC 周期键。
package periods

import (
	"errors"
	"time"
)

const (
	Day       = "day"
	Week      = "week"
	Month     = "month"
	Year      = "year" // 查询视角，不是持久化表中的周期种类。
	All       = "all"
	AllStart  = "0001-01-01"
	dayLayout = "2006-01-02"
)

var ErrInvalidDay = errors.New("invalid UTC period day")

// LeaderboardDay selects a day in the previous complete week or month.
// Other kinds keep the current UTC day. ForDay itself always finds the period
// containing its input, as required by settlement and retention.
func LeaderboardDay(kind string, now time.Time) string {
	u := now.UTC()
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	switch kind {
	case Week:
		return today.AddDate(0, 0, -(int(today.Weekday())+6)%7-1).Format(dayLayout)
	case Month:
		return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).Format(dayLayout)
	default:
		return today.Format(dayLayout)
	}
}

// Boundaries 返回包含给定 UTC 日的左闭右开周、月边界。
type Boundaries struct {
	Day                  string
	WeekStart, WeekEnd   string
	MonthStart, MonthEnd string
}

func ForDay(day string) (Boundaries, error) {
	d, err := time.Parse(dayLayout, day)
	if err != nil || len(day) != len(dayLayout) || d.Format(dayLayout) != day {
		return Boundaries{}, ErrInvalidDay
	}
	week := d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
	month := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
	b := Boundaries{Day: day, WeekStart: week.Format(dayLayout), WeekEnd: week.AddDate(0, 0, 7).Format(dayLayout), MonthStart: month.Format(dayLayout), MonthEnd: month.AddDate(0, 1, 0).Format(dayLayout)}
	if len(b.WeekStart) != 10 || len(b.WeekEnd) != 10 || len(b.MonthStart) != 10 || len(b.MonthEnd) != 10 {
		return Boundaries{}, ErrInvalidDay
	}
	return b, nil
}
