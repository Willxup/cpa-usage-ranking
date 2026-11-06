package test

import (
	"errors"
	"math"
	"testing"

	"cpa-usage-ranking/internal/reports"
)

func number(v float64) *float64 { return &v }

func TestValidateMetrics(t *testing.T) {
	base := reports.Item{Model: "unknown", TotalTokens: 20, InputTokens: 12, OutputTokens: 6,
		CacheReadTokens: 4, CacheWriteTokens: 3, ReasoningTokens: 2, RequestCount: 3, SuccessCount: 2,
		TTFTAvgMS: number(0), TTFTSamples: 1, LatencyAvgMS: number(1.5), LatencySamples: 3}
	if err := reports.Validate(reports.Report{Items: []reports.Item{base, {}}}); err != nil {
		t.Fatalf("valid metrics: %v", err)
	}
	max := int64(math.MaxInt64)
	boundary := reports.Item{TotalTokens: max, InputTokens: max, CacheReadTokens: max, RequestCount: max,
		TTFTAvgMS: number(1), TTFTSamples: max}
	if err := reports.Validate(reports.Report{Revision: max, Items: []reports.Item{boundary}}); err != nil {
		t.Fatalf("max int64 rejected: %v", err)
	}
	cases := map[string]func(*reports.Item){
		"negative token":                 func(i *reports.Item) { i.TotalTokens = -1 },
		"negative count":                 func(i *reports.Item) { i.SuccessCount = -1 },
		"success above requests":         func(i *reports.Item) { i.SuccessCount = 4 },
		"ttft samples above requests":    func(i *reports.Item) { i.TTFTSamples = 4 },
		"latency samples above requests": func(i *reports.Item) { i.LatencySamples = 4 },
		"cache sum above input":          func(i *reports.Item) { i.CacheReadTokens = 10 },
		"cache sum overflow":             func(i *reports.Item) { i.CacheReadTokens = max; i.CacheWriteTokens = 1 },
		"reasoning above output":         func(i *reports.Item) { i.ReasoningTokens = 7 },
		"input output above total":       func(i *reports.Item) { i.TotalTokens = 17 },
		"input output overflow":          func(i *reports.Item) { i.InputTokens = max; i.OutputTokens = 1; i.TotalTokens = max },
		"usage without requests":         func(i *reports.Item) { *i = reports.Item{TotalTokens: 1} },
		"zero samples non-null":          func(i *reports.Item) { i.TTFTSamples = 0 },
		"positive samples null":          func(i *reports.Item) { i.LatencyAvgMS = nil },
		"negative average":               func(i *reports.Item) { i.LatencyAvgMS = number(-1) },
		"nan average":                    func(i *reports.Item) { i.TTFTAvgMS = number(math.NaN()) },
		"positive infinity":              func(i *reports.Item) { i.LatencyAvgMS = number(math.Inf(1)) },
		"negative infinity":              func(i *reports.Item) { i.TTFTAvgMS = number(math.Inf(-1)) },
		"multiplication overflow":        func(i *reports.Item) { i.LatencyAvgMS = number(math.MaxFloat64) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := base
			mutate(&bad)
			if err := reports.Validate(reports.Report{Items: []reports.Item{base, bad}}); !errors.Is(err, reports.ErrInvalid) {
				t.Fatalf("inconsistent unknown model accepted: %v", err)
			}
		})
	}
	if err := reports.Validate(reports.Report{Revision: -1}); !errors.Is(err, reports.ErrInvalid) {
		t.Fatalf("negative revision accepted: %v", err)
	}
}
