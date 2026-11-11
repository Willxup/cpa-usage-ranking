package capacity

// Conservative stage arithmetic and search decisions. This file does not
// start a service, implement a watchdog, or authorize any remote load.

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	SustainedStageDuration   = 3 * time.Minute
	SustainedWindowDuration  = time.Minute
	SustainedSearchBudget    = 40 * time.Minute
	SustainedConfirmDuration = 20 * time.Minute
	SustainedTotalBudget     = time.Hour
)

type SustainedStageResult struct {
	Verdict         string  `json:"verdict"` // candidate, failed, uncertain, tool_limited, policy_limited, capacity_policy, safety_stop
	Reason          string  `json:"reason"`
	TargetRate      float64 `json:"target_rate"`
	ArrivalRate     float64 `json:"arrival_rate"`
	ProducedRate    float64 `json:"produced_rate"` // includes separately marked debt
	ConfirmedRate   float64 `json:"confirmed_rate"`
	StartPending    int64   `json:"start_pending"`
	EndPending      int64   `json:"end_pending"`
	PeakPending     int64   `json:"peak_pending"`
	MarkedDebt      int64   `json:"marked_debt"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type SustainedStageSignals struct {
	ToolFailure  bool `json:"tool_failure"`
	SafetyStop   bool `json:"safety_stop"`
	CacheFailure bool `json:"cache_failure"`
	DataFailure  bool `json:"data_failure"`
}

func sampleCodeDelta(a, b map[string]int64, code string) int64 { return b[code] - a[code] }

// AssessSustainedStage consumes the cumulative sample immediately before the
// stage and consecutive samples from this one stage. Timestamps are wall-clock
// event times, not nominal sleep durations. The caller must additionally watch
// generator/SSH/host resources and communicate failures via signals.
func AssessSustainedStage(before SustainedSample, samples []SustainedSample, rate float64, signals SustainedStageSignals) (SustainedStageResult, error) {
	return assessSustainedStage(before, samples, rate, signals, SustainedStageDuration, SustainedWindowDuration)
}

func assessSustainedStage(before SustainedSample, samples []SustainedSample, rate float64, signals SustainedStageSignals, duration, window time.Duration) (SustainedStageResult, error) {
	out := SustainedStageResult{TargetRate: rate, StartPending: before.Pending}
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) || duration <= 0 || window <= 0 || duration < 2*window || len(samples) < 3 {
		return out, errors.New("invalid stage assessment input")
	}
	previous := before
	for _, s := range samples {
		if s.TimeUnixNS <= previous.TimeUnixNS || s.Produced < previous.Produced || s.ArrivalProduced < previous.ArrivalProduced || s.DebtProduced < previous.DebtProduced || s.Confirmed < previous.Confirmed || s.Pending != s.Produced-s.Confirmed || s.Confirmed > s.Produced || s.TargetRate != rate || s.Stage != samples[0].Stage {
			return out, errors.New("non-monotonic or mismatched stage samples")
		}
		previous = s
	}
	end := samples[len(samples)-1]
	elapsed := time.Duration(end.TimeUnixNS - before.TimeUnixNS)
	if elapsed <= 0 {
		return out, errors.New("invalid stage duration")
	}
	out.DurationSeconds = elapsed.Seconds()
	out.ArrivalRate = float64(end.ArrivalProduced-before.ArrivalProduced) / elapsed.Seconds()
	out.ProducedRate = float64(end.Produced-before.Produced) / elapsed.Seconds()
	out.ConfirmedRate = float64(end.Confirmed-before.Confirmed) / elapsed.Seconds()
	out.EndPending = end.Pending
	out.MarkedDebt = end.DebtProduced - before.DebtProduced
	out.PeakPending = before.Pending
	for _, s := range samples {
		if s.Pending > out.PeakPending {
			out.PeakPending = s.Pending
		}
	}
	if signals.SafetyStop {
		out.Verdict, out.Reason = "safety_stop", "host or external watchdog stopped the run"
		return out, nil
	}
	if signals.ToolFailure {
		out.Verdict, out.Reason = "tool_limited", "generator or transport path was not valid"
		return out, nil
	}
	if sampleCodeDelta(before.ReportErrorCodes, end.ReportErrorCodes, "report_rate_limited") > 0 || sampleCodeDelta(before.RegistrationErrorCodes, end.RegistrationErrorCodes, "registration_rate_limited") > 0 || sampleCodeDelta(before.RegistrationErrorCodes, end.RegistrationErrorCodes, "creation_quota_exceeded") > 0 {
		out.Verdict, out.Reason = "policy_limited", "test rate admission was reached"
		return out, nil
	}
	if sampleCodeDelta(before.ReportErrorCodes, end.ReportErrorCodes, "report_capacity") > 0 || sampleCodeDelta(before.RegistrationErrorCodes, end.RegistrationErrorCodes, "registration_capacity") > 0 {
		out.Verdict, out.Reason = "capacity_policy", "non-rate admission capacity was reached"
		return out, nil
	}
	if signals.CacheFailure || signals.DataFailure {
		out.Verdict, out.Reason = "failed", "cache publication or data correctness failed"
		return out, nil
	}
	if elapsed < duration || out.MarkedDebt != int64(math.Round(5*rate)) || end.DebtRemaining != 0 {
		out.Verdict, out.Reason = "uncertain", "stage duration or marked-debt observation incomplete"
		return out, nil
	}
	if out.ArrivalRate < 0.95*rate {
		out.Verdict, out.Reason = "tool_limited", "actual arrival input was below 95 percent of target"
		return out, nil
	}
	// Samples nearest at or before the two preceding window boundaries. Limit
	// missing samples so an irregular stream cannot masquerade as two windows.
	atOrBefore := func(target int64) (SustainedSample, bool) {
		for i := len(samples) - 1; i >= 0; i-- {
			if samples[i].TimeUnixNS <= target {
				return samples[i], target-samples[i].TimeUnixNS <= int64(window/10)
			}
		}
		return SustainedSample{}, false
	}
	mid, okMid := atOrBefore(end.TimeUnixNS - int64(window))
	start, okStart := atOrBefore(end.TimeUnixNS - int64(2*window))
	if !okMid || !okStart {
		out.Verdict, out.Reason = "uncertain", "insufficient contiguous window samples"
		return out, nil
	}
	growing := func(a, b SustainedSample) bool {
		return b.Produced-a.Produced > b.Confirmed-a.Confirmed && b.Pending > a.Pending && b.OldestPendingMS > a.OldestPendingMS
	}
	if growing(start, mid) && growing(mid, end) {
		out.Verdict, out.Reason = "failed", "two consecutive windows have rising debt and oldest age"
		return out, nil
	}
	cleared := false
	for _, s := range samples {
		if s.DebtRemaining == 0 && s.Pending == 0 {
			cleared = true
		}
	}
	netDown := end.Pending < out.PeakPending && end.Pending < mid.Pending && mid.Pending <= start.Pending
	// A zero observed after all marked work was produced proves the old and
	// marked debts cleared. Subsequent ordinary in-flight work need not be
	// zero at the exact last sample. A short tail is conservatively bounded
	// by the size of the predeclared 5-lambda marker (a count, not a latency
	// SLO); the two growing-window failure above still takes precedence.
	shortTailAfterClear := cleared && end.Pending <= out.MarkedDebt
	if shortTailAfterClear || netDown {
		out.Verdict, out.Reason = "candidate", "debt cleared with a short tail or the recent backlog is decreasing"
		return out, nil
	}
	out.Verdict, out.Reason = "uncertain", "window slopes or debt recovery do not give a clear decision"
	return out, nil
}

type SustainedSearchPlan struct {
	Current          int    `json:"current"`
	Lower            int    `json:"lower"`
	Upper            int    `json:"upper"` // zero means no observed upper bound
	UncertainRepeats int    `json:"uncertain_repeats"`
	Done             bool   `json:"done"`
	Reason           string `json:"reason,omitempty"`
}

type SustainedSearchStep struct {
	Action string `json:"action"` // stage, transition_to_confirmation, stop
	Rate   int    `json:"rate,omitempty"`
	Reason string `json:"reason"`
}

func NewSustainedSearchPlan() SustainedSearchPlan { return SustainedSearchPlan{Current: 100} }

// Advance changes rates only after an observed stage. SearchElapsed is the
// cumulative active search time across segments, never wall time spent in a
// planned transition. The actual execution layer must persist and enforce it.
func (p *SustainedSearchPlan) Advance(verdict string, searchElapsed time.Duration) (SustainedSearchStep, error) {
	if p.Done || p.Current < 1 || p.Lower < 0 || p.Upper < 0 || p.Upper > 0 && p.Upper <= p.Lower || searchElapsed < 0 || searchElapsed > SustainedSearchBudget {
		return SustainedSearchStep{}, errors.New("invalid or exhausted search state")
	}
	stop := func(reason string) SustainedSearchStep {
		p.Done, p.Reason = true, reason
		return SustainedSearchStep{Action: "stop", Reason: reason}
	}
	confirm := func(reason string) SustainedSearchStep {
		if p.Lower == 0 {
			return stop("no demonstrated candidate: " + reason)
		}
		p.Done, p.Reason = true, reason
		return SustainedSearchStep{Action: "transition_to_confirmation", Rate: p.Lower, Reason: reason}
	}
	switch verdict {
	case "candidate":
		if p.Current > p.Lower {
			p.Lower = p.Current
		}
		p.UncertainRepeats = 0
	case "failed":
		if p.Upper == 0 || p.Current < p.Upper {
			p.Upper = p.Current
		}
		p.UncertainRepeats = 0
	case "uncertain":
		if p.UncertainRepeats == 0 && searchElapsed+SustainedStageDuration <= SustainedSearchBudget {
			p.UncertainRepeats++
			return SustainedSearchStep{Action: "stage", Rate: p.Current, Reason: "repeat uncertain stage once"}, nil
		}
		return confirm("unresolved uncertain stage"), nil
	case "tool_limited", "policy_limited", "capacity_policy", "safety_stop":
		return stop(verdict), nil
	default:
		return SustainedSearchStep{}, fmt.Errorf("unknown search verdict %q", verdict)
	}
	if p.Upper > 0 && p.Lower > 0 && float64(p.Upper-p.Lower) <= math.Max(25, 0.1*float64(p.Lower)) {
		return confirm("observed interval reached target precision"), nil
	}
	if searchElapsed+SustainedStageDuration > SustainedSearchBudget {
		return confirm("search budget reserved for continuous confirmation"), nil
	}
	if p.Upper == 0 {
		if p.Current > 50000 {
			return stop("generator rate safety bound reached without an upper bound"), nil
		}
		p.Current *= 2
	} else {
		p.Current = p.Lower + (p.Upper-p.Lower)/2
		if p.Current < 1 {
			return stop("no demonstrated candidate above the minimum test rate"), nil
		}
	}
	return SustainedSearchStep{Action: "stage", Rate: p.Current, Reason: "continue search"}, nil
}
