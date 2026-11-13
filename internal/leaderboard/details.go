package leaderboard

import (
	"fmt"
	"strconv"

	"cpa-usage-ranking/internal/statistics"
)

// OverallDetails 只公开总榜已确认的六项数据，来自评分使用的同一份周期合计。
// 比例为百分数；没有分母或测量样本时为 null，不以零代替未知。
type OverallDetails struct {
	TotalTokens   string  `json:"total_tokens"`
	RequestCount  string  `json:"request_count"`
	SuccessRate   string  `json:"success_rate"`
	CacheReadRate *string `json:"cache_read_rate"`
	TTFTMeanMS    *string `json:"ttft_mean_ms"`
	LatencyMeanMS *string `json:"latency_mean_ms"`
}

func percentage(numerator, denominator int64) string {
	value := roundRatio(numerator, denominator, 10000)
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

func measuredMean(value statistics.Optional) *string {
	if !value.Valid {
		return nil
	}
	text := strconv.FormatFloat(round2(value.Value), 'f', 2, 64)
	return &text
}

func overallDetails(c *candidate) *OverallDetails {
	m := c.metrics
	d := &OverallDetails{
		TotalTokens:   strconv.FormatInt(m.TotalTokens, 10),
		RequestCount:  strconv.FormatInt(m.RequestCount, 10),
		SuccessRate:   percentage(m.SuccessCount, m.RequestCount),
		TTFTMeanMS:    measuredMean(c.derived.TTFTMeanMS),
		LatencyMeanMS: measuredMean(c.derived.LatencyMeanMS),
	}
	if m.InputTokens > 0 {
		value := percentage(m.CacheReadTokens, m.InputTokens)
		d.CacheReadRate = &value
	}
	return d
}
