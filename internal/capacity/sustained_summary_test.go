package capacity

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSustainedSummaryDelayOverHourIsExplicit(t *testing.T) {
	models := []string{"model"}
	encoded, _ := json.Marshal(models)
	hash := sha256.Sum256(encoded)
	path := filepath.Join(t.TempDir(), "journal")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(file)
	for _, record := range []sustainedRecord{
		{Kind: "header", Header: &sustainedHeader{Version: SustainedModelVersion, Day: "2026-11-13", CatalogHash: hex.EncodeToString(hash[:])}},
		{Kind: "stage", Command: &SustainedCommand{Operation: "stage", Stage: "s", Rate: 100}},
		{Kind: "created", Source: "arrival", PlannedUnixNS: 1, Task: &SustainedTask{Sequence: 1, Client: 1, Day: "2026-11-13", Body: json.RawMessage(`{"items":[{}]}`), Deltas: []SustainedDelta{{Model: "model", Tier: "default", RequestCount: 1}}}},
		{Kind: "start", Sequence: 1, Action: "report"},
		{Kind: "finish", Sequence: 1, Action: "report", Confirmed: true, TimeUnixNS: 3700000000001},
		{Kind: "end"},
	} {
		if err = enc.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	stats, err := SummarizeSustained(path, models, 1)
	if err != nil || len(stats.Stages) != 1 || stats.Stages[0].DelayOverHour != 1 || stats.Stages[0].ConfirmDelayMS.P99 != -1 {
		t.Fatalf("over-hour delay was reported as an upper bound: %+v %v", stats, err)
	}
}

func TestSustainedSummaryAttributesLateConfirmationsAndBounds(t *testing.T) {
	models := []string{"model-a", "model-b"}
	catalog, _ := json.Marshal(models)
	hash := sha256.Sum256(catalog)
	header := &sustainedHeader{Version: SustainedModelVersion, Day: "2026-11-13", CatalogHash: hex.EncodeToString(hash[:])}
	task := func(seq int64, model, tier string) *SustainedTask {
		return &SustainedTask{Sequence: seq, Client: int(seq), Day: "2026-11-13", Body: json.RawMessage(`{"items":[{},{}]}`),
			Deltas: []SustainedDelta{{Model: model, Tier: tier, RequestCount: 3}}}
	}
	records := []sustainedRecord{
		{Kind: "header", Header: header},
		{Kind: "stage", Command: &SustainedCommand{Operation: "stage", Stage: "a", Rate: 100}},
		{Kind: "created", Source: "debt", PlannedUnixNS: 1, Task: task(1, "model-a", "default")},
		{Kind: "start", Sequence: 1, Action: "report"},
		{Kind: "stage", Command: &SustainedCommand{Operation: "stage", Stage: "b", Rate: 200}},
		{Kind: "created", Source: "arrival", PlannedUnixNS: 1, Task: task(2, "model-b", "flex")},
		{Kind: "start", Sequence: 2, Action: "report"},
		{Kind: "finish", Sequence: 1, Action: "report", Confirmed: true, TimeUnixNS: 101000001},
		{Kind: "finish", Sequence: 2, Action: "report", Confirmed: true, TimeUnixNS: 120000001},
		{Kind: "end"},
	}
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzip"}[compressed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			var writer interface{ Write([]byte) (int, error) } = file
			var z *gzip.Writer
			if compressed {
				z = gzip.NewWriter(file)
				writer = z
			}
			enc := json.NewEncoder(writer)
			for _, record := range records {
				if err = enc.Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			if z != nil {
				if err = z.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			stats, err := SummarizeSustained(path, models, 2)
			if err != nil || stats.Produced != 2 || stats.Confirmed != 2 || stats.Pending != 0 || len(stats.Stages) != 2 {
				t.Fatalf("bad summary: %+v %v", stats, err)
			}
			if stats.Stages[0].Confirmed != 1 || stats.Stages[0].DebtTasks != 1 || stats.Stages[0].TouchedClients != 1 || stats.Stages[0].TodayTasks != 1 || stats.Stages[0].ModelCalls["model-a"] != 3 || stats.Stages[1].TierCalls["flex"] != 3 || stats.Stages[0].ConfirmDelayMS.P50 != 110 || stats.Stages[1].ConfirmDelayMS.P99 != 120 {
				t.Fatalf("bad per-stage attribution/buckets: %+v", stats.Stages)
			}
			if _, err = SummarizeSustained(path, models, 1); err == nil {
				t.Fatal("pending bound ignored")
			}
			if _, err = SummarizeSustained(path, []string{"wrong"}, 2); err == nil {
				t.Fatal("catalog mismatch ignored")
			}
		})
	}
}
