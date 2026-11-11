package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"cpa-usage-ranking/internal/capacity"
)

// sustainedAssessment is a deliberately thin JSON bridge: all classification
// and search decisions remain in the shared capacity package.
type sustainedAssessment struct {
	Mode            string                          `json:"mode"`
	Before          capacity.SustainedSample        `json:"before"`
	Samples         []capacity.SustainedSample      `json:"samples"`
	Rate            float64                         `json:"rate"`
	Signals         capacity.SustainedStageSignals  `json:"signals"`
	Plan            capacity.SustainedSearchPlan    `json:"plan"`
	SearchElapsedNS int64                           `json:"search_elapsed_ns"`
	StartUnixNS     int64                           `json:"start_unix_ns"`
	Cache           []capacity.SustainedCacheChange `json:"cache"`
	Paused          bool                            `json:"paused"`
}

type sustainedAssessmentResult struct {
	Stage        *capacity.SustainedStageResult  `json:"stage,omitempty"`
	Confirmation *capacity.SustainedConfirmation `json:"confirmation,omitempty"`
	Plan         *capacity.SustainedSearchPlan   `json:"plan,omitempty"`
	Step         *capacity.SustainedSearchStep   `json:"step,omitempty"`
}

func assessSustainedInput(in io.Reader) (sustainedAssessmentResult, error) {
	var req sustainedAssessment
	var out sustainedAssessmentResult
	dec := json.NewDecoder(io.LimitReader(in, 32<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return out, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return out, errors.New("assessment input must contain exactly one JSON object")
	}
	switch req.Mode {
	case "stage":
		v, err := capacity.AssessSustainedStage(req.Before, req.Samples, req.Rate, req.Signals)
		if err != nil {
			return out, err
		}
		if req.Rate != float64(req.Plan.Current) {
			return out, errors.New("assessment rate does not match search plan")
		}
		step, err := req.Plan.Advance(v.Verdict, time.Duration(req.SearchElapsedNS))
		if err != nil {
			return out, err
		}
		out.Stage, out.Plan, out.Step = &v, &req.Plan, &step
	case "confirmation":
		v, err := capacity.AssessSustainedConfirmation(req.Before, req.Samples, req.StartUnixNS, req.Rate, req.Cache, req.Paused, req.Signals)
		if err != nil {
			return out, err
		}
		out.Confirmation = &v
	default:
		return out, fmt.Errorf("unknown sustained assessment mode %q", req.Mode)
	}
	return out, nil
}

// A separate small sample stream avoids repeatedly scanning the large journal.
// The journal remains authoritative. A write failure cancels the driver and is
// returned as an error; the controller also detects missing/stale samples.
func runSustainedSamples(ctx context.Context, f capacity.Fixture, o capacity.SustainedOptions, commands <-chan capacity.SustainedCommand, path string) (capacity.SustainedResult, error) {
	if path == "" {
		return capacity.RunSustained(ctx, f, o, commands, nil)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return capacity.SustainedResult{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan capacity.SustainedSample, 8)
	finished := make(chan error, 1)
	go func() {
		var writeErr error
		enc := json.NewEncoder(file)
		for sample := range ch {
			if writeErr == nil {
				writeErr = enc.Encode(sample)
				if writeErr != nil {
					cancel()
				}
			}
		}
		if err := file.Sync(); writeErr == nil {
			writeErr = err
		}
		if err := file.Close(); writeErr == nil {
			writeErr = err
		}
		finished <- writeErr
	}()
	result, runErr := capacity.RunSustained(ctx, f, o, commands, ch)
	close(ch)
	return result, errors.Join(runErr, <-finished)
}
