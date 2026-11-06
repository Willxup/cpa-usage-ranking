package registration

import (
	"fmt"
	"time"
)

type CreationLimits struct {
	Hourly     int
	Daily      int
	MaxSources int
}

func DefaultCreationLimits() CreationLimits {
	return CreationLimits{Hourly: 3, Daily: 10, MaxSources: 10000}
}

func (l CreationLimits) Validate() error {
	if l.Hourly <= 0 || l.Daily <= 0 || l.MaxSources <= 0 {
		return fmt.Errorf("registration creation limits must be positive")
	}
	return nil
}

// checkCreationQuota runs under Service.mu. Its only mutations reclaim expired
// state; successful database insertion, not admission, appends a creation time.
func (s *Service) checkCreationQuota(source string, now time.Time) error {
	dayCutoff := now.Add(-24 * time.Hour)
	events := s.creations[source]
	first := 0
	for first < len(events) && !events[first].After(dayCutoff) {
		first++
	}
	if first > 0 {
		// 复制保留部分，避免长期持有过期事件的底层数组。
		events = append([]time.Time(nil), events[first:]...)
		if len(events) == 0 {
			delete(s.creations, source)
		} else {
			s.creations[source] = events
		}
	}
	if len(events) == 0 && len(s.creations) >= s.limits.MaxSources {
		var earliest time.Time
		// 容量紧张时才扫描有界来源表。全过期记录可回收，活跃记录不能逐出。
		for key, times := range s.creations {
			expires := times[len(times)-1].Add(24 * time.Hour)
			if !now.Before(expires) {
				delete(s.creations, key)
				continue
			}
			if earliest.IsZero() || expires.Before(earliest) {
				earliest = expires
			}
		}
		if len(s.creations) >= s.limits.MaxSources {
			return &LimitError{Code: "registration_capacity", RetryAfter: earliest.Sub(now)}
		}
	}
	var until time.Time
	if len(events) >= s.limits.Daily {
		until = events[0].Add(24 * time.Hour)
	}
	hourCutoff := now.Add(-time.Hour)
	hourStart := 0
	for hourStart < len(events) && !events[hourStart].After(hourCutoff) {
		hourStart++
	}
	if len(events)-hourStart >= s.limits.Hourly {
		hourExpiry := events[hourStart].Add(time.Hour)
		if hourExpiry.After(until) {
			until = hourExpiry
		}
	}
	if !until.IsZero() {
		return &LimitError{Code: "creation_quota_exceeded", RetryAfter: until.Sub(now)}
	}
	return nil
}
