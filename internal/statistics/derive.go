package statistics

import (
	"errors"
	"math"

	"cpa-usage-ranking/internal/reports"
)

var ErrInvalidMetrics = errors.New("invalid raw statistics")

// Optional 区分真实的零和不可计算；原始 int64 不通过此类型转换。
type Optional struct {
	Value float64
	Valid bool
}

type Derived struct {
	CacheReadRate, CacheWriteRate, ReasoningRate, OutputRate Optional
	NonReasoningOutput                                       int64
	TotalPerRequest, OutputPerRequest, SuccessRate           Optional
	TTFTMeanMS, LatencyMeanMS, TTFTCoverage, LatencyCoverage Optional
	OutputRatePerSecond, OutputRateExcludingTTFTPerSecond    Optional
}

// Derive 只从原始合计计算；速度不是并发吞吐，也不是逐请求速率的算术均值。
// 非有限结果返回错误，不将溢出伪装为缺失或零。
func Derive(m reports.Metrics) (Derived, error) {
	for _, v := range []int64{m.TotalTokens, m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheWriteTokens, m.ReasoningTokens, m.RequestCount, m.SuccessCount, m.TTFTSamples, m.LatencySamples} {
		if v < 0 {
			return Derived{}, ErrInvalidMetrics
		}
	}
	if m.ReasoningTokens > m.OutputTokens || m.SuccessCount > m.RequestCount || m.TTFTSamples > m.RequestCount || m.LatencySamples > m.RequestCount ||
		!finiteNonnegative(m.TTFTSumMS) || !finiteNonnegative(m.LatencySumMS) ||
		(m.TTFTSamples == 0 && m.TTFTSumMS != 0) || (m.LatencySamples == 0 && m.LatencySumMS != 0) {
		return Derived{}, ErrInvalidMetrics
	}
	var d Derived
	d.NonReasoningOutput = m.OutputTokens - m.ReasoningTokens
	d.CacheReadRate = ratio(float64(m.CacheReadTokens), float64(m.InputTokens))
	d.CacheWriteRate = ratio(float64(m.CacheWriteTokens), float64(m.InputTokens))
	d.ReasoningRate = ratio(float64(m.ReasoningTokens), float64(m.OutputTokens))
	d.OutputRate = ratio(float64(m.OutputTokens), float64(m.TotalTokens))
	d.TotalPerRequest = ratio(float64(m.TotalTokens), float64(m.RequestCount))
	d.OutputPerRequest = ratio(float64(m.OutputTokens), float64(m.RequestCount))
	d.SuccessRate = ratio(float64(m.SuccessCount), float64(m.RequestCount))
	d.TTFTMeanMS = ratio(m.TTFTSumMS, float64(m.TTFTSamples))
	d.LatencyMeanMS = ratio(m.LatencySumMS, float64(m.LatencySamples))
	d.TTFTCoverage = ratio(float64(m.TTFTSamples), float64(m.RequestCount))
	d.LatencyCoverage = ratio(float64(m.LatencySamples), float64(m.RequestCount))
	if m.RequestCount > 0 && m.LatencySamples == m.RequestCount {
		d.OutputRatePerSecond = ratio(float64(m.OutputTokens)*1000, m.LatencySumMS)
		if m.TTFTSamples == m.RequestCount && m.LatencySumMS > m.TTFTSumMS {
			d.OutputRateExcludingTTFTPerSecond = ratio(float64(m.OutputTokens)*1000, m.LatencySumMS-m.TTFTSumMS)
		}
	}
	if (d.OutputRatePerSecond.Valid && !finiteNonnegative(d.OutputRatePerSecond.Value)) ||
		(d.OutputRateExcludingTTFTPerSecond.Valid && !finiteNonnegative(d.OutputRateExcludingTTFTPerSecond.Value)) {
		return Derived{}, ErrOverflow
	}
	return d, nil
}

func ratio(n, denominator float64) Optional {
	if denominator == 0 {
		return Optional{}
	}
	return Optional{Value: n / denominator, Valid: true}
}
func finiteNonnegative(n float64) bool { return n >= 0 && !math.IsInf(n, 0) && !math.IsNaN(n) }
