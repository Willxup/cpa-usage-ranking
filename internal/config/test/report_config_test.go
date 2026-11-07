package test

import (
	"testing"
	"time"

	"cpa-usage-ranking/internal/config"
)

func TestReportConfigurationOverrides(t *testing.T) {
	clearEnvironment(t)
	for key, value := range map[string]string{
		"RANKING_REPORT_MAX_BODY_BYTES": "2048", "RANKING_REPORT_MAX_ITEMS": "22", "RANKING_REPORT_DECODE_CONCURRENCY": "3",
		"RANKING_REPORT_QUEUE_CAPACITY": "7", "RANKING_REPORT_TRANSACTION_TIMEOUT": "9s", "RANKING_REPORT_CLIENT_INTERVAL": "2m",
		"RANKING_REPORT_CLIENT_BURST": "4", "RANKING_REPORT_GLOBAL_INTERVAL": "12ms", "RANKING_REPORT_GLOBAL_BURST": "33",
		"RANKING_REPORT_MAX_CLIENTS": "99", "RANKING_REPORT_STATE_TTL": "15m",
	} {
		t.Setenv(key, value)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.ReportIngestion
	if l.MaxBodyBytes != 2048 || l.MaxItems != 22 || l.ConcurrentDecodes != 3 || l.ClientInterval != 2*time.Minute || l.ClientBurst != 4 || l.GlobalInterval != 12*time.Millisecond || l.GlobalBurst != 33 || l.MaxClients != 99 || l.StateTTL != 15*time.Minute || cfg.ReportWriter.QueueCapacity != 7 || cfg.ReportWriter.TransactionTimeout != 9*time.Second {
		t.Fatalf("report overrides were not applied: %+v %+v", l, cfg.ReportWriter)
	}
}
