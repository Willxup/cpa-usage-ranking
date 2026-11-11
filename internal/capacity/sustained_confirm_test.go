package capacity

import (
	"testing"
	"time"
)

// These short, simulated windows exercise the arithmetic only. They are not
// real load or a 20-minute confirmation.
func TestSustainedConfirmationBranchesWithSimulatedSamples(t *testing.T) {
	before, samples := stageSamples("candidate")
	for i := range samples {
		samples[i].ActiveSeconds = float64(i+1) / 4
	}
	start := before.TimeUnixNS
	cache := []SustainedCacheChange{
		{ObservedUnixNS: start + int64(500*time.Millisecond), Generation: "a"},
		{ObservedUnixNS: start + int64(1500*time.Millisecond), Generation: "b"},
		{ObservedUnixNS: start + int64(2500*time.Millisecond), Generation: "c"},
	}
	evaluate := func(s []SustainedSample, c []SustainedCacheChange, paused bool) SustainedConfirmation {
		t.Helper()
		v, err := assessSustainedConfirmation(before, s, start, 10, c, paused, SustainedStageSignals{}, 3*time.Second, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if out := evaluate(samples, cache, false); out.Verdict != "confirmed" || out.CacheChanges != 3 || out.Windows != 2 {
		t.Fatalf("valid simulated confirmation: %+v", out)
	}
	if out := evaluate(samples, cache[:2], false); out.Verdict != "unconfirmed" {
		t.Fatalf("missing cache publication claimed confirmation: %+v", out)
	}
	if out := evaluate(samples, cache, true); out.Verdict != "unconfirmed" {
		t.Fatalf("paused confirmation accepted: %+v", out)
	}
	stalled := append([]SustainedSample(nil), samples...)
	stalled[len(stalled)-1].ActiveSeconds--
	if out := evaluate(stalled, cache, false); out.Verdict != "unconfirmed" {
		t.Fatalf("hidden pause accepted: %+v", out)
	}
	badCache := append([]SustainedCacheChange(nil), cache...)
	badCache[2].Generation = "a"
	if _, err := assessSustainedConfirmation(before, samples, start, 10, badCache, false, SustainedStageSignals{}, 3*time.Second, time.Second); err == nil {
		t.Fatal("repeated cache generation accepted")
	}
}

func TestSustainedConfirmationAllowsTransientGrowthBeforeClear(t *testing.T) {
	before, samples := stageSamples("candidate")
	for i := range samples {
		n := int64(i + 1)
		pending := int64(15) + n
		if n > 9 {
			pending = (12 - n) * 8
		}
		samples[i].Pending = pending
		samples[i].Confirmed = samples[i].Produced - pending
		samples[i].OldestPendingMS = float64(n * 250)
		samples[i].ActiveSeconds = float64(n) / 4
	}
	start := before.TimeUnixNS
	cache := []SustainedCacheChange{{start + int64(500*time.Millisecond), "a"}, {start + int64(1500*time.Millisecond), "b"}, {start + int64(2500*time.Millisecond), "c"}}
	out, err := assessSustainedConfirmation(before, samples, start, 10, cache, false, SustainedStageSignals{}, 3*time.Second, time.Second)
	if err != nil || out.Verdict != "confirmed" {
		t.Fatalf("recovery after temporary growth was rejected: %+v %v", out, err)
	}
}

func TestSustainedConfirmationSlowDeclineWithNoise(t *testing.T) {
	base := time.Unix(1000, 0).UnixNano()
	before := SustainedSample{TimeUnixNS: base, Produced: 50000, Confirmed: 40000, Pending: 10000}
	var samples []SustainedSample
	for i := int64(1); i <= 20; i++ {
		pending := int64(10500) - i // one task per window, with 500 marked tasks still included
		if i == 3 {
			pending += 4
		} else if i == 4 {
			pending += 6 // two temporary increases do not override the long decline
		}
		produced := int64(50500) + 100*i
		samples = append(samples, SustainedSample{TimeUnixNS: base + i*int64(time.Second), Stage: "confirmation", TargetRate: 100,
			Produced: produced, ArrivalProduced: 100 * i, DebtProduced: 500, Confirmed: produced - pending,
			Pending: pending, OldestPendingMS: float64(i * 1000), ActiveSeconds: float64(i)})
	}
	cache := []SustainedCacheChange{{base + int64(2*time.Second), "a"}, {base + int64(8*time.Second), "b"}, {base + int64(16*time.Second), "c"}}
	evaluate := func(s []SustainedSample) SustainedConfirmation {
		t.Helper()
		v, err := assessSustainedConfirmation(before, s, base, 100, cache, false, SustainedStageSignals{}, 20*time.Second, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := evaluate(samples); got.Verdict != "confirmed" || got.Stage.EndPending == 0 {
		t.Fatalf("slow noisy decline with residual debt rejected: %+v", got)
	}
	growing := append([]SustainedSample(nil), samples...)
	for i := range growing {
		pending := int64(10500 + i)
		growing[i].Pending = pending
		growing[i].Confirmed = growing[i].Produced - pending
	}
	if got := evaluate(growing); got.Verdict != "failed" {
		t.Fatalf("continuous growth was not rejected: %+v", got)
	}
	flat := append([]SustainedSample(nil), samples...)
	for i := range flat {
		flat[i].Pending = 10500
		flat[i].Confirmed = flat[i].Produced - flat[i].Pending
	}
	if got := evaluate(flat); got.Verdict != "unconfirmed" {
		t.Fatalf("flat debt was mistaken for recovery: %+v", got)
	}
	interrupted := append([]SustainedSample(nil), samples...)
	for i := range interrupted {
		// One window without arrivals is compensated by the next window;
		// the full-run average remains 100/s and the debt still falls.
		if i == 7 {
			interrupted[i].ArrivalProduced -= 100
			interrupted[i].Produced -= 100
			interrupted[i].Confirmed -= 100
		}
	}
	if got := evaluate(interrupted); got.Verdict != "tool_limited" {
		t.Fatalf("interrupted input was hidden by the overall average: %+v", got)
	}
	short := append([]SustainedSample(nil), samples...)
	for i := range short {
		short[i].ArrivalProduced = int64(i+1) * 50
		short[i].Produced = 50500 + short[i].ArrivalProduced
		short[i].Confirmed = short[i].Produced - short[i].Pending
	}
	if got := evaluate(short); got.Verdict != "tool_limited" {
		t.Fatalf("insufficient actual input accepted: %+v", got)
	}
}
