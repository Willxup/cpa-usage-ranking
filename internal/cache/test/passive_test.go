package test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-ranking/internal/cache"
	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/periods"
	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/statistics"
)

func TestReportAndSettlementDoNotTriggerCacheBuild(t *testing.T) {
	db, r := cacheFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"currency":"USD","price_unit":"per_million_tokens","models":[{"model_name":"alpha","alias":[],"prices":{}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := models.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := models.Register(ctx, db.Writer, catalog)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	item := cache.Item{ID: "fixture", Kind: periods.All, Encode: func(_ context.Context, _ statistics.Snapshot, w io.Writer) error {
		calls.Add(1)
		_, err := io.WriteString(w, "snapshot")
		return err
	}}
	c := newCache(t, r, []cache.Item{item}, time.Hour)
	_, _ = run(t, c)
	waitFor(t, func() bool { _, err := c.Current(); return err == nil })
	old, _ := c.Current()
	p, err := reports.Prepare(reports.Report{Revision: 2, Items: []reports.Item{{Model: "alpha", TotalTokens: 9, InputTokens: 9, RequestCount: 1, SuccessCount: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	store := reports.NewStore(db.Writer, registry, func() time.Time { return now })
	if _, err = store.Apply(ctx, 1, "2026-12-31", p); err != nil {
		t.Fatal(err)
	}
	if err = reports.AdvanceWindow(ctx, db.Writer, time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, "2026-12-31", 1); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err = db.Writer.Exec(`UPDATE clients SET display_name='renamed',disabled=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	snapshot, err := c.Current()
	before, _ := old.Get("fixture")
	after, _ := snapshot.Get("fixture")
	if err != nil || before != after || calls.Load() != 1 {
		t.Fatal("mutation triggered cache refresh", calls.Load(), err)
	}
}
