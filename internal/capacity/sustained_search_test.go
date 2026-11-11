package capacity

import (
	"math"
	"testing"
	"time"
)

func stageSamples(mode string) (SustainedSample, []SustainedSample) {
	base := time.Unix(1000, 0).UnixNano()
	before := SustainedSample{TimeUnixNS: base}
	var samples []SustainedSample
	for i := int64(1); i <= 12; i++ {
		arrivals := i * 5 / 2 // 10 per second, sampled every 250ms
		if mode == "input_short" {
			arrivals = i * 2
		}
		produced := 50 + arrivals
		var pending int64
		var age float64
		switch mode {
		case "failed":
			pending, age = 40+i, float64(i*250)
		case "uncertain":
			pending, age = 40+i, 1000
		default:
			pending, age = 48-4*i, float64(48-4*i)
		}
		s := SustainedSample{TimeUnixNS: base + i*int64(250*time.Millisecond), Stage: "s", TargetRate: 10, Produced: produced, ArrivalProduced: arrivals, DebtProduced: 50, Confirmed: produced - pending, Pending: pending, OldestPendingMS: age}
		if mode == "policy" && i == 12 {
			s.ReportErrorCodes = map[string]int64{"report_rate_limited": 1}
		}
		samples = append(samples, s)
	}
	return before, samples
}

func TestSustainedStageConservativeClassifications(t *testing.T) {
	for _, tt := range []struct{ input, verdict string }{{"candidate", "candidate"}, {"failed", "failed"}, {"uncertain", "uncertain"}, {"input_short", "tool_limited"}, {"policy", "policy_limited"}} {
		t.Run(tt.input, func(t *testing.T) {
			before, samples := stageSamples(tt.input)
			got, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{}, 3*time.Second, time.Second)
			if err != nil || got.Verdict != tt.verdict || got.MarkedDebt != 50 {
				t.Fatalf("got %+v, %v; want %s", got, err, tt.verdict)
			}
		})
	}
	before, samples := stageSamples("candidate")
	if got, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{SafetyStop: true}, 3*time.Second, time.Second); err != nil || got.Verdict != "safety_stop" {
		t.Fatalf("safety failure not preserved: %+v %v", got, err)
	}
	if got, err := assessSustainedStage(before, samples[:8], 10, SustainedStageSignals{}, 3*time.Second, time.Second); err != nil || got.Verdict != "uncertain" {
		t.Fatalf("incomplete observation claimed a result: %+v %v", got, err)
	}
	samples[6].Confirmed = samples[5].Confirmed - 1
	if _, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{}, 3*time.Second, time.Second); err == nil {
		t.Fatal("accepted inconsistent sample sequence")
	}
}

func TestSustainedStageAllowsShortTailAfterObservedClear(t *testing.T) {
	before, samples := stageSamples("candidate")
	last := &samples[len(samples)-1]
	// An earlier sample reached zero after marker production, then normal
	// in-flight work appeared before the final snapshot.
	samples[len(samples)-2].Pending = 0
	samples[len(samples)-2].Confirmed = samples[len(samples)-2].Produced
	last.Pending = 3
	last.Confirmed = last.Produced - last.Pending
	got, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{}, 3*time.Second, time.Second)
	if err != nil || got.Verdict != "candidate" {
		t.Fatalf("short in-flight tail after observed clear: %+v %v", got, err)
	}
}

func TestSustainedClearDoesNotMaskTwoGrowingWindows(t *testing.T) {
	before, samples := stageSamples("candidate")
	for i := 3; i < len(samples); i++ {
		pending := int64(i - 3)
		samples[i].Pending = pending
		samples[i].Confirmed = samples[i].Produced - pending
		samples[i].OldestPendingMS = float64(pending * 250)
	}
	got, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{}, 3*time.Second, time.Second)
	if err != nil || got.Verdict != "failed" {
		t.Fatalf("prior clear masked sustained growth: %+v %v", got, err)
	}
}

func TestSustainedClearDoesNotMaskRisingDebtWithFlatAge(t *testing.T) {
	before, samples := stageSamples("candidate")
	for i := 3; i < len(samples); i++ {
		pending := int64(i - 3)
		samples[i].Pending = pending
		samples[i].Confirmed = samples[i].Produced - pending
		samples[i].OldestPendingMS = 1000 // age alone does not certify recovery
	}
	got, err := assessSustainedStage(before, samples, 10, SustainedStageSignals{}, 3*time.Second, time.Second)
	if err != nil || got.Verdict == "candidate" {
		t.Fatalf("prior clear masked rising debt with flat age: %+v %v", got, err)
	}
}

// The fixed 95% gate is intentionally conservative at the initial rate:
// E[A]=18000 and the one-sided Poisson Chernoff bound is exp(-22.5).
// Lower rates can have substantially more stochastic variance; the result is
// tool_limited, never an asserted application upper bound.
func TestSustainedInitialRatePoissonInputGate(t *testing.T) {
	const rate = 100.0
	const seconds = 180.0
	for seed := uint64(1); seed <= 100; seed++ {
		rng := splitMix{state: seed}
		elapsed, count := 0.0, 0
		for {
			u := (float64(rng.next()>>11) + 0.5) / (1 << 53)
			elapsed += -math.Log(u) / rate
			if elapsed > seconds {
				break
			}
			count++
		}
		if float64(count)/seconds < 0.95*rate {
			t.Fatalf("seed %d: generated only %d arrivals", seed, count)
		}
	}
}

func TestSustainedSearchBracketAndConfirmation(t *testing.T) {
	p := NewSustainedSearchPlan()
	for _, step := range []struct {
		verdict string
		want    int
	}{{"candidate", 200}, {"candidate", 400}, {"candidate", 800}, {"failed", 600}, {"candidate", 700}, {"failed", 650}} {
		out, err := p.Advance(step.verdict, 3*time.Minute)
		if err != nil || out.Action != "stage" || out.Rate != step.want {
			t.Fatalf("after %s: %+v %v, plan %+v", step.verdict, out, err, p)
		}
	}
	out, err := p.Advance("failed", 21*time.Minute)
	if err != nil || out.Action != "transition_to_confirmation" || out.Rate != 600 || p.Lower != 600 || p.Upper != 650 {
		t.Fatalf("bad bracket/confirmation: %+v %v, plan %+v", out, err, p)
	}
	if _, err = p.Advance("candidate", 24*time.Minute); err == nil {
		t.Fatal("accepted search work after confirmation decision")
	}
}

func TestSustainedSearchUncertainBudgetAndLimits(t *testing.T) {
	p := NewSustainedSearchPlan()
	out, err := p.Advance("failed", 3*time.Minute)
	if err != nil || out.Action != "stage" || out.Rate != 50 {
		t.Fatalf("did not explore below first overloaded stage: %+v %v", out, err)
	}
	out, err = p.Advance("candidate", 6*time.Minute)
	if err != nil || out.Rate != 75 {
		t.Fatalf("did not narrow bracket: %+v %v", out, err)
	}
	out, err = p.Advance("uncertain", 9*time.Minute)
	if err != nil || out.Action != "stage" || out.Rate != 75 {
		t.Fatalf("uncertain observation not repeated: %+v %v", out, err)
	}
	out, err = p.Advance("uncertain", 12*time.Minute)
	if err != nil || out.Action != "transition_to_confirmation" || out.Rate != 50 {
		t.Fatalf("uncertainty converted into a boundary: %+v %v", out, err)
	}
	p = NewSustainedSearchPlan()
	out, err = p.Advance("candidate", 39*time.Minute)
	if err != nil || out.Action != "transition_to_confirmation" || out.Rate != 100 {
		t.Fatalf("did not reserve confirmation time: %+v %v", out, err)
	}
	for _, verdict := range []string{"tool_limited", "policy_limited", "capacity_policy", "safety_stop"} {
		p = NewSustainedSearchPlan()
		out, err = p.Advance(verdict, 3*time.Minute)
		if err != nil || out.Action != "stop" || out.Reason != verdict {
			t.Fatalf("%s confused with capacity: %+v %v", verdict, out, err)
		}
	}
}
