package test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/leaderboard"
	"cpa-usage-ranking/internal/reports"
)

func TestOverallDetailsSameAggregateAndWhitelist(t *testing.T) {
	s := snapshot()
	add(&s, 1, reports.Metrics{TotalTokens: 9007199254740993, RequestCount: 800, SuccessCount: 201, InputTokens: 800, CacheReadTokens: 201, TTFTSamples: 3, TTFTSumMS: 31, LatencySamples: 2, LatencySumMS: 401})
	boards := build(t, s)
	d := boards[leaderboard.Overall][0].Details
	if d == nil || d.TotalTokens != "9007199254740993" || d.RequestCount != "800" || d.SuccessRate != "25.13" || d.CacheReadRate == nil || *d.CacheReadRate != "25.13" || d.TTFTMeanMS == nil || *d.TTFTMeanMS != "10.33" || d.LatencyMeanMS == nil || *d.LatencyMeanMS != "200.50" {
		t.Fatalf("wrong details: %+v", d)
	}
	if d.SuccessRate == boards[leaderboard.Success][0].Metric {
		t.Fatal("raw success percentage confused with Wilson score")
	}
	for board, entries := range boards {
		for _, entry := range entries {
			encoded, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var public map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &public); err != nil {
				t.Fatal(err)
			}
			if board != leaderboard.Overall {
				if entry.Details != nil || len(public) != 5 {
					t.Fatalf("details leaked to %s: %s", board, encoded)
				}
				continue
			}
			if len(public) != 6 {
				t.Fatal(string(encoded))
			}
			var details map[string]json.RawMessage
			if err := json.Unmarshal(public["details"], &details); err != nil {
				t.Fatal(err)
			}
			if len(details) != 6 {
				t.Fatal(details)
			}
			for _, key := range []string{"total_tokens", "request_count", "success_rate", "cache_read_rate", "ttft_mean_ms", "latency_mean_ms"} {
				if details[key] == nil {
					t.Fatalf("missing %s", key)
				}
			}
		}
	}
}

func TestOverallDetailsMissingZeroAndExtremes(t *testing.T) {
	s := snapshot()
	add(&s, 1, reports.Metrics{RequestCount: 1})
	add(&s, 2, reports.Metrics{RequestCount: 1, InputTokens: 1, TTFTSamples: 1, LatencySamples: 1})
	add(&s, 3, reports.Metrics{RequestCount: math.MaxInt64, SuccessCount: math.MaxInt64, TotalTokens: math.MaxInt64, InputTokens: math.MaxInt64, CacheReadTokens: math.MaxInt64, TTFTSamples: 1, TTFTSumMS: math.MaxFloat64, LatencySamples: 1, LatencySumMS: math.MaxFloat64})
	rows := build(t, s)[leaderboard.Overall]
	missing, zero, large := find(t, rows, 1).Details, find(t, rows, 2).Details, find(t, rows, 3).Details
	if missing == nil || missing.CacheReadRate != nil || missing.TTFTMeanMS != nil || missing.LatencyMeanMS != nil || missing.SuccessRate != "0.00" || missing.TotalTokens != "0" {
		t.Fatalf("missing became zero: %+v", missing)
	}
	if zero == nil || zero.CacheReadRate == nil || *zero.CacheReadRate != "0.00" || zero.TTFTMeanMS == nil || *zero.TTFTMeanMS != "0.00" || zero.LatencyMeanMS == nil || *zero.LatencyMeanMS != "0.00" {
		t.Fatalf("zero became missing: %+v", zero)
	}
	if large == nil || large.TotalTokens != "9223372036854775807" || large.RequestCount != "9223372036854775807" || large.SuccessRate != "100.00" || *large.CacheReadRate != "100.00" {
		t.Fatalf("precision lost: %+v", large)
	}
	encoded, err := json.Marshal(large)
	if err != nil || strings.Contains(string(encoded), "Inf") || strings.Contains(string(encoded), "NaN") {
		t.Fatalf("invalid extreme mean: %s %v", encoded, err)
	}
	encoded, err = json.Marshal(missing)
	if err != nil || !strings.Contains(string(encoded), `"ttft_mean_ms":null`) || !strings.Contains(string(encoded), `"cache_read_rate":null`) {
		t.Fatalf("missing not explicit null: %s %v", encoded, err)
	}
}
