package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cpa-usage-ranking/internal/capacity"
)

func assessmentInput(t *testing.T, in sustainedAssessment) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}

func TestSustainedAssessmentBridge(t *testing.T) {
	base := int64(time.Second)
	before := capacity.SustainedSample{TimeUnixNS: base}
	var samples []capacity.SustainedSample
	for i := int64(1); i <= 1200; i++ {
		pending := int64(500) - i*5
		if pending < 0 {
			pending = 0
		}
		produced := i*100 + 500
		samples = append(samples, capacity.SustainedSample{
			TimeUnixNS: base + i*int64(time.Second), StageStartUnixNS: base,
			Stage: "test", TargetRate: 100, Produced: produced, ArrivalProduced: i * 100,
			DebtProduced: 500, Confirmed: produced - pending, Pending: pending,
			OldestPendingMS: float64(pending), ActiveSeconds: float64(i),
		})
	}
	request := sustainedAssessment{Mode: "stage", Before: before, Samples: samples[:180], Rate: 100, Plan: capacity.NewSustainedSearchPlan(), SearchElapsedNS: int64(180 * time.Second)}
	out, err := assessSustainedInput(assessmentInput(t, request))
	if err != nil || out.Stage == nil || out.Stage.Verdict != "candidate" || out.Plan == nil || out.Plan.Lower != 100 || out.Step == nil || out.Step.Rate != 200 {
		t.Fatalf("stage bridge: %+v, %v", out, err)
	}
	// The queue cleared earlier; normal short in-flight work at the very end
	// must not make a continuous confirmation fail merely for being nonzero.
	for i := 1000; i < len(samples); i++ {
		samples[i].Pending = 10
		samples[i].Confirmed = samples[i].Produced - 10
	}
	request = sustainedAssessment{Mode: "confirmation", Before: before, Samples: samples, Rate: 100, StartUnixNS: base,
		Cache: []capacity.SustainedCacheChange{{ObservedUnixNS: base + 300*int64(time.Second), Generation: "a"}, {ObservedUnixNS: base + 600*int64(time.Second), Generation: "b"}, {ObservedUnixNS: base + 900*int64(time.Second), Generation: "c"}}}
	out, err = assessSustainedInput(assessmentInput(t, request))
	if err != nil || out.Confirmation == nil || out.Confirmation.Verdict != "confirmed" {
		t.Fatalf("confirmation bridge: %+v, %v", out, err)
	}
	request.Cache = request.Cache[:2]
	out, err = assessSustainedInput(assessmentInput(t, request))
	if err != nil || out.Confirmation.Verdict != "unconfirmed" {
		t.Fatalf("cache evidence incorrectly accepted: %+v, %v", out, err)
	}
}

func TestSustainedAssessmentInputIsStrict(t *testing.T) {
	for _, input := range []string{`{"mode":"unknown"}`, `{"mode":"stage","extra":1}`, `{} {}`} {
		if _, err := assessSustainedInput(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted invalid input %q", input)
		}
	}
}
