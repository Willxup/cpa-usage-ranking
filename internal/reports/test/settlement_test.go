package test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/reports"
)

func TestSettlementPeriodBoundariesAndSampleSums(t *testing.T) {
	db := reportDB(t)
	registry := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}},{"model_name":"beta","alias":[],"prices":{}}`)
	client, other := createReportClient(t, db), createReportClient(t, db)
	now := instant("2026-10-31T12:00:00Z")
	store := reports.NewStore(db.Writer, registry, func() time.Time { return now })
	ctx := context.Background()
	put := func(id int64, day string, items ...reports.Item) {
		t.Helper()
		now = instant(day + "T12:00:00Z")
		if _, err := store.Apply(ctx, id, day, prepared(t, 1, items...)); err != nil {
			t.Fatal(err)
		}
	}
	a := usage("alpha", models.Default, 10, 2, 3)
	a.LatencyAvgMS, a.LatencySamples = number(8), 1
	b := usage("alpha", models.Default, 20, 3, 7)
	b.LatencyAvgMS, b.LatencySamples = number(4), 3
	priority := usage("alpha", models.Priority, 5, 1, 10)
	beta := usage("beta", models.Flex, 7, 1, 2)
	put(client, "2026-10-31", a, priority, beta)
	put(other, "2026-10-31", a)
	put(client, "2026-11-01", b)
	put(client, "2026-12-31", a)
	put(other, "2026-12-31", usage("unknown", models.Default, 5, 1, 1)) // 全过滤快照也要删除版本。
	put(client, "2027-01-01", b)
	put(other, "2027-01-01") // 空快照也要删除版本。
	if err := reports.AdvanceWindow(ctx, db.Writer, instant("2027-01-03T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		day string
		id  int64
	}{{"2026-10-31", client}, {"2026-10-31", other}, {"2026-11-01", client}, {"2026-12-31", client}, {"2026-12-31", other}, {"2027-01-01", client}, {"2027-01-01", other}} {
		ok, err := reports.Finalize(ctx, db.Writer, item.day, item.id)
		if err != nil || !ok {
			t.Fatalf("finalize %+v: %v %v", item, ok, err)
		}
		ok, err = reports.Finalize(ctx, db.Writer, item.day, item.id)
		if err != nil || ok {
			t.Fatalf("repeat %+v: %v %v", item, ok, err)
		}
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM daily_reports`) != 0 || scalar(t, db.Reader, `SELECT count(*) FROM daily_stats`) != 0 {
		t.Fatal("source not deleted")
	}
	alpha, _ := registry.CanonicalID("alpha")
	q := `SELECT total_tokens FROM period_stats WHERE kind=? AND start_day=? AND client_id=? AND model_id=? AND tier=?`
	for _, x := range []struct {
		kind, start           string
		id, model, tier, want int64
	}{
		{"week", "2026-10-26", client, 0, 0, 42}, {"week", "2026-10-26", client, alpha, 0, 30}, {"week", "2026-10-26", client, alpha, 1, 5},
		{"month", "2026-10-01", client, 0, 0, 22}, {"month", "2026-11-01", client, 0, 0, 20},
		{"week", "2026-12-28", client, 0, 0, 30}, {"month", "2026-12-01", client, 0, 0, 10}, {"month", "2027-01-01", client, 0, 0, 20},
		{"all", "0001-01-01", client, 0, 0, 72}, {"all", "0001-01-01", other, 0, 0, 10}, {"day", "2026-10-31", client, 0, 0, 22},
	} {
		if got := scalar(t, db.Reader, q, x.kind, x.start, x.id, x.model, x.tier); got != x.want {
			t.Errorf("%+v got %d", x, got)
		}
	}
	var sum float64
	var samples int64
	if err := db.Reader.QueryRow(`SELECT ttft_sum_ms,ttft_samples FROM period_stats WHERE kind='week' AND start_day='2026-10-26' AND client_id=? AND model_id=? AND tier=0`, client, alpha).Scan(&sum, &samples); err != nil || sum != 27 || samples != 5 {
		t.Fatalf("TTFT sum %v samples %d: %v", sum, samples, err)
	}
	if err := db.Reader.QueryRow(`SELECT latency_sum_ms,latency_samples FROM period_stats WHERE kind='week' AND start_day='2026-10-26' AND client_id=? AND model_id=? AND tier=0`, client, alpha).Scan(&sum, &samples); err != nil || sum != 20 || samples != 4 {
		t.Fatalf("latency sum %v samples %d: %v", sum, samples, err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day' AND (model_id<>0 OR client_id=? AND start_day IN ('2026-12-31','2027-01-01'))`, other) != 0 {
		t.Fatal("model daily row or empty daily summary created")
	}
}

func TestSettlementUsesPersistedClosedWatermark(t *testing.T) {
	db := reportDB(t)
	reg := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}}`)
	id := createReportClient(t, db)
	ctx := context.Background()
	now := instant("2026-11-01T10:00:00Z")
	store := reports.NewStore(db.Writer, reg, func() time.Time { return now })
	if _, err := store.Apply(ctx, id, "2026-11-01", prepared(t, 1, usage("alpha", 0, 4, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id); err != nil || ok {
		t.Fatalf("open day settled %v %v", ok, err)
	}
	if _, err := store.Apply(ctx, id, "2026-10-31", prepared(t, 1, usage("alpha", 0, 2, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, "2026-10-31", id); err != nil || ok {
		t.Fatalf("day equal to watermark settled %v %v", ok, err)
	}
	if err := reports.AdvanceWindow(ctx, db.Writer, instant("2026-11-03T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	now = instant("2026-10-31T12:00:00Z")
	if err := reports.AdvanceWindow(ctx, db.Writer, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id); err != nil || !ok {
		t.Fatalf("closed day not settled %v %v", ok, err)
	}
	if _, err := store.Apply(ctx, id, "2026-11-01", prepared(t, 2)); !errors.Is(err, reports.ErrFutureDay) && !errors.Is(err, reports.ErrExpiredDay) {
		t.Fatalf("rollback reopened day: %v", err)
	}
}

func TestSettlementSQLiteFailureRollsBackWritesAndDeletes(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER fail BEFORE INSERT ON period_stats WHEN NEW.kind='month' BEGIN SELECT RAISE(ABORT,'write failure'); END`,
		`CREATE TRIGGER fail BEFORE DELETE ON daily_reports BEGIN SELECT RAISE(ABORT,'delete failure'); END`,
	} {
		t.Run(trigger, func(t *testing.T) {
			db := reportDB(t)
			reg := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}}`)
			id := createReportClient(t, db)
			ctx := context.Background()
			store := reports.NewStore(db.Writer, reg, func() time.Time { return instant("2026-11-01T12:00:00Z") })
			if _, err := store.Apply(ctx, id, "2026-11-01", prepared(t, 1, usage("alpha", 0, 4, 1, 1))); err != nil {
				t.Fatal(err)
			}
			if err := reports.AdvanceWindow(ctx, db.Writer, instant("2026-11-03T00:00:00Z")); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Writer.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id); err == nil || ok {
				t.Fatalf("fault: %v %v", ok, err)
			}
			if scalar(t, db.Reader, `SELECT count(*) FROM period_stats`) != 0 || scalar(t, db.Reader, `SELECT count(*) FROM daily_stats`) != 2 || scalar(t, db.Reader, `SELECT count(*) FROM daily_reports`) != 1 {
				t.Fatal("partial transaction escaped")
			}
			if _, err := db.Writer.Exec(`DROP TRIGGER fail`); err != nil {
				t.Fatal(err)
			}
			if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id); err != nil || !ok {
				t.Fatalf("retry: %v %v", ok, err)
			}
		})
	}
}

func TestSettlementOverflowPreservesSource(t *testing.T) {
	for _, field := range []string{"total_tokens", "ttft_sum_ms"} {
		t.Run(field, func(t *testing.T) {
			db := reportDB(t)
			reg := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}}`)
			id := createReportClient(t, db)
			ctx := context.Background()
			now := instant("2026-11-01T12:00:00Z")
			store := reports.NewStore(db.Writer, reg, func() time.Time { return now })
			for _, day := range []string{"2026-11-01", "2026-11-02"} {
				now = instant(day + "T12:00:00Z")
				if _, err := store.Apply(ctx, id, day, prepared(t, 1, usage("alpha", 0, 4, 1, 1))); err != nil {
					t.Fatal(err)
				}
			}
			if err := reports.AdvanceWindow(ctx, db.Writer, instant("2026-11-04T12:00:00Z")); err != nil {
				t.Fatal(err)
			}
			if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id); err != nil || !ok {
				t.Fatal(ok, err)
			}
			value := any(int64(math.MaxInt64))
			if field == "ttft_sum_ms" {
				value = math.MaxFloat64
				if _, err := db.Writer.Exec(`UPDATE daily_stats SET ttft_sum_ms=? WHERE day='2026-11-02'`, math.MaxFloat64); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Writer.Exec(`UPDATE period_stats SET `+field+`=? WHERE kind='month' AND model_id=0`, value); err != nil {
				t.Fatal(err)
			}
			if ok, err := reports.Finalize(ctx, db.Writer, "2026-11-02", id); !errors.Is(err, reports.ErrSettlementOverflow) || ok {
				t.Fatalf("overflow: %v %v", ok, err)
			}
			if scalar(t, db.Reader, `SELECT count(*) FROM daily_reports WHERE day='2026-11-02'`) != 1 || scalar(t, db.Reader, `SELECT count(*) FROM daily_stats WHERE day='2026-11-02'`) != 2 {
				t.Fatal("overflow lost source")
			}
			if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week' AND start_day='2026-11-02' AND model_id=0`) != 0 {
				t.Fatal("earlier week write escaped rollback")
			}
		})
	}
}

func TestShanghaiMidnightClosesSnapshotAfterRevisionAndRetry(t *testing.T) {
	db := reportDB(t)
	registry := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}}`)
	client := createReportClient(t, db)
	ctx := context.Background()
	now := instant("2026-12-31T15:59:59Z")
	store := reports.NewStore(db.Writer, registry, func() time.Time { return now })
	day := "2026-12-30"
	for _, rev := range []int64{1, 2, 2} {
		if _, err := store.Apply(ctx, client, day, prepared(t, rev, usage("alpha", 0, rev*10, 1, 1))); err != nil {
			t.Fatal(err)
		}
	}
	received := scalar(t, db.Reader, `SELECT received_at_ms FROM daily_reports WHERE day=?`, day)
	if received != now.UnixMilli() {
		t.Fatal("received_at not the original instant", received)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, day, client); err != nil || ok {
		t.Fatalf("open day finalized: %v %v", ok, err)
	}
	now = instant("2026-12-31T16:00:00Z")
	if _, err := store.Apply(ctx, client, day, prepared(t, 2, usage("alpha", 0, 20, 1, 1))); !errors.Is(err, reports.ErrExpiredDay) {
		t.Fatalf("closed retry: %v", err)
	}
	if ok, err := reports.Finalize(ctx, db.Writer, day, client); err != nil || !ok {
		t.Fatalf("closed day not finalized: %v %v", ok, err)
	}
	if scalar(t, db.Reader, `SELECT total_tokens FROM period_stats WHERE kind='all' AND model_id=0 AND client_id=?`, client) != 20 {
		t.Fatal("revisions or retry double-counted")
	}
	if ok, err := reports.Finalize(ctx, db.Writer, day, client); err != nil || ok {
		t.Fatalf("settled twice: %v %v", ok, err)
	}
}
