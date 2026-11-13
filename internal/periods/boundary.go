// Package periods 定义日结使用的 上海业务日周期键。
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

var ErrInvalidDay = errors.New("invalid business period day")

// Boundaries 返回包含给定业务日的左闭右开周、月边界。
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
