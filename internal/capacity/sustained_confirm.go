package capacity

import (
	"errors"
	"time"
)

// SustainedCacheChange must come from an independently observed actual cache
// generation change, not from a sleep or a predicted five-minute schedule.
type SustainedCacheChange struct {
	ObservedUnixNS int64  `json:"observed_unix_ns"`
	Generation     string `json:"generation"`
}

type SustainedConfirmation struct {
	Verdict      string               `json:"verdict"` // confirmed, failed, unconfirmed, or a limit
	Reason       string               `json:"reason"`
	Stage        SustainedStageResult `json:"stage"`
	CacheChanges int                  `json:"cache_changes"`
	Windows      int                  `json:"windows"`
}

// AssessSustainedConfirmation is an offline decision aid, not a safety guard.
// before is the last cumulative sample before stage start; startUnixNS is the
// actual recorded start event. Any planned transition during confirmation is a
// disqualification. The caller must provide independently recorded cache data.
func AssessSustainedConfirmation(before SustainedSample, samples []SustainedSample, startUnixNS int64, rate float64, cache []SustainedCacheChange, paused bool, signals SustainedStageSignals) (SustainedConfirmation, error) {
	return assessSustainedConfirmation(before, samples, startUnixNS, rate, cache, paused, signals, SustainedConfirmDuration, SustainedWindowDuration)
}

func assessSustainedConfirmation(before SustainedSample, samples []SustainedSample, startUnixNS int64, rate float64, cache []SustainedCacheChange, paused bool, signals SustainedStageSignals, duration, window time.Duration) (SustainedConfirmation, error) {
	var out SustainedConfirmation
	if len(samples) < 3 || startUnixNS < before.TimeUnixNS || startUnixNS > samples[0].TimeUnixNS {
		return out, errors.New("invalid confirmation start or samples")
	}
	stage, err := assessSustainedStage(before, samples, rate, signals, duration, window)
	if err != nil {
		return out, err
	}
	out.Stage = stage
	end := samples[len(samples)-1]
	switch stage.Verdict {
	case "tool_limited", "policy_limited", "capacity_policy", "safety_stop", "failed":
		out.Verdict, out.Reason = stage.Verdict, stage.Reason
		return out, nil
	}
	if paused || end.TimeUnixNS-startUnixNS < int64(duration) {
		out.Verdict, out.Reason = "unconfirmed", "confirmation was paused or shorter than the continuous requirement"
		return out, nil
	}
	// A pause can otherwise be hidden between one-second samples. Require the
	// active-time counter to follow wall time within a bounded sampling margin.
	wall := time.Duration(end.TimeUnixNS - samples[0].TimeUnixNS)
	active := time.Duration((end.ActiveSeconds - samples[0].ActiveSeconds) * float64(time.Second))
	margin := window / 10
	if margin > 2*time.Second {
		margin = 2 * time.Second
	}
	if active < 0 || wall-active > margin || active-wall > margin {
		out.Verdict, out.Reason = "unconfirmed", "active time is inconsistent with a continuous confirmation"
		return out, nil
	}
	lastAt := int64(0)
	seen := map[string]bool{}
	for _, c := range cache {
		if c.ObservedUnixNS < startUnixNS || c.ObservedUnixNS > end.TimeUnixNS || c.ObservedUnixNS <= lastAt || c.Generation == "" || seen[c.Generation] {
			return out, errors.New("invalid cache generation change sequence")
		}
		lastAt = c.ObservedUnixNS
		seen[c.Generation] = true
		out.CacheChanges++
	}
	if out.CacheChanges < 3 {
		out.Verdict, out.Reason = "unconfirmed", "fewer than three observed cache generation changes"
		return out, nil
	}
	// Judge the whole post-marker observation, not an isolated pair of noisy
	// windows. A slow net decline needs no minimum speed and need not clear;
	// the majority of windows and the latter half must both show recovery.
	trend, valid := measureSustainedDebtTrend(before, samples, stage.MarkedDebt, rate, window)
	out.Windows = trend.Windows
	if !valid || out.Windows < int(duration/window)-1 || stage.Verdict != "candidate" {
		out.Verdict, out.Reason = "unconfirmed", "insufficient windows or debt recovery remains uncertain"
		return out, nil
	}
	if !trend.Declining && !(trend.Cleared && (end.Pending == 0 || end.Pending <= stage.MarkedDebt && !trend.LastTwoGrowing)) {
		out.Verdict, out.Reason = "unconfirmed", "post-marker debt trend does not demonstrate stable recovery"
		return out, nil
	}
	out.Verdict, out.Reason = "confirmed", "continuous input, post-marker net debt recovery and observed cache changes met the checks"
	return out, nil
}
