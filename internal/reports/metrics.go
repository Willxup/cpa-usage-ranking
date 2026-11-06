package reports

import "math"

// Metrics is the shared storage and aggregation representation. Averages are
// converted to sums with their actual sample counts, never guessed from requests.
type Metrics struct {
	TotalTokens      int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	RequestCount     int64
	SuccessCount     int64
	TTFTSumMS        float64
	TTFTSamples      int64
	LatencySumMS     float64
	LatencySamples   int64
}

// Validate rejects the whole report on any inconsistent item, regardless of
// whether its model will later be matched or ignored.
func Validate(report Report) error {
	if report.Revision < 0 {
		return ErrInvalid
	}
	for _, item := range report.Items {
		if _, err := itemMetrics(item); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

func itemMetrics(item Item) (Metrics, error) {
	m := Metrics{
		TotalTokens: item.TotalTokens, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens,
		CacheReadTokens: item.CacheReadTokens, CacheWriteTokens: item.CacheWriteTokens, ReasoningTokens: item.ReasoningTokens,
		RequestCount: item.RequestCount, SuccessCount: item.SuccessCount,
		TTFTSamples: item.TTFTSamples, LatencySamples: item.LatencySamples,
	}
	for _, n := range []int64{m.TotalTokens, m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheWriteTokens,
		m.ReasoningTokens, m.RequestCount, m.SuccessCount, m.TTFTSamples, m.LatencySamples} {
		if n < 0 {
			return Metrics{}, ErrInvalid
		}
	}
	if m.SuccessCount > m.RequestCount || m.TTFTSamples > m.RequestCount || m.LatencySamples > m.RequestCount {
		return Metrics{}, ErrInvalid
	}
	cache, ok := addInt(m.CacheReadTokens, m.CacheWriteTokens)
	if !ok || cache > m.InputTokens || m.ReasoningTokens > m.OutputTokens {
		return Metrics{}, ErrInvalid
	}
	classified, ok := addInt(m.InputTokens, m.OutputTokens)
	if !ok || classified > m.TotalTokens || (m.TotalTokens > 0 && m.RequestCount == 0) {
		return Metrics{}, ErrInvalid
	}
	var err error
	if m.TTFTSumMS, err = durationSum(item.TTFTAvgMS, m.TTFTSamples); err != nil {
		return Metrics{}, err
	}
	if m.LatencySumMS, err = durationSum(item.LatencyAvgMS, m.LatencySamples); err != nil {
		return Metrics{}, err
	}
	return m, nil
}

func durationSum(avg *float64, samples int64) (float64, error) {
	if samples == 0 {
		if avg != nil {
			return 0, ErrInvalid
		}
		return 0, nil
	}
	if avg == nil || math.IsNaN(*avg) || math.IsInf(*avg, 0) || *avg < 0 {
		return 0, ErrInvalid
	}
	sum := *avg * float64(samples)
	if math.IsInf(sum, 0) || math.IsNaN(sum) {
		return 0, ErrInvalid
	}
	return sum, nil
}

func addInt(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}
