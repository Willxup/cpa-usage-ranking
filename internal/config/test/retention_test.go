package test

import (
	"testing"

	"cpa-usage-ranking/internal/config"
)

func TestRetentionOverride(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("RANKING_SETTLED_DAY_RETENTION_DAYS", "14")
	cfg, err := config.Load()
	if err != nil || cfg.DailyRetentionDays != 14 {
		t.Fatalf("retention: %d, %v", cfg.DailyRetentionDays, err)
	}
}
