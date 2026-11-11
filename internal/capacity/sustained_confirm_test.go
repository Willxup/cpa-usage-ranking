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

func TestSustainedConfirmationFindsEarlierGrowingWindows(t *testing.T) {
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
	if err != nil || out.Verdict != "failed" {
		t.Fatalf("earlier growing windows not detected: %+v %v", out, err)
	}
}
