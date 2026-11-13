package test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"cpa-usage-ranking/internal/periods"
	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/storage"
)

func periodFixture(t *testing.T, db *storage.DB, kind, day string, id, model, tier int64) {
	t.Helper()
	if _, err := db.Writer.Exec(`INSERT INTO period_stats VALUES(?,?,?,?,?,4,4,0,0,0,0,1,1,1,1,0,0)`, kind, day, model, tier, id); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionShanghaiInclusiveBoundariesAndSourceProtection(t *testing.T) {
	db := reportDB(t)
	id := seedClosedReports(t, db, 1)[0]
	ctx := context.Background()
	for _, day := range []string{"2026-10-14", "2026-10-15", "2026-10-16", "2026-11-15"} {
		periodFixture(t, db, periods.Day, day, id, 0, 0)
	}
	for _, day := range []string{"2026-10-19", "2026-10-26", "2026-11-02", "2026-11-09"} {
		periodFixture(t, db, periods.Week, day, id, 0, 0)
		periodFixture(t, db, periods.Week, day, id, 1, 1)
	}
	periodFixture(t, db, periods.Month, "2026-01-01", id, 0, 0)
	periodFixture(t, db, periods.All, periods.AllStart, id, 0, 0)
	now := instant("2026-11-15T15:59:59Z")
	for _, kind := range []string{periods.Day, periods.Week} {
		n, err := reports.Prune(ctx, db.Writer, now, 31, kind, 1)
		if err != nil || n != 1 {
			t.Fatalf("first %s: %d %v", kind, n, err)
		}
	}
	if got := scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day' AND start_day<'2026-10-16'`); got != 1 {
		t.Fatalf("not bounded %d", got)
	}
	for _, kind := range []string{periods.Day, periods.Week} {
		for {
			n, err := reports.Prune(ctx, db.Writer, now, 31, kind, 2)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			if n > 2 {
				t.Fatal("oversized delete")
			}
		}
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day' AND start_day='2026-10-16'`) != 1 || scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week' AND start_day IN ('2026-11-02','2026-11-09')`) != 4 {
		t.Fatal("inclusive retained boundary lost")
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind IN ('month','all')`) != 2 || scalar(t, db.Reader, `SELECT count(*) FROM daily_reports`) != 1 || scalar(t, db.Reader, `SELECT count(*) FROM daily_stats`) != 2 {
		t.Fatal("retention deleted long term or unsettled data")
	}
	// UTC 仍是周日，但已到上海周一：应保留 11-09 和 11-16 两周。
	later, err := time.Parse(time.RFC3339, "2026-11-15T16:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Prune(ctx, db.Writer, later, 31, periods.Week, 20); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week' AND start_day='2026-11-02'`) != 0 || scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week' AND start_day='2026-11-09'`) != 2 {
		t.Fatal("Shanghai monday boundary wrong")
	}
	if _, err := reports.Prune(ctx, db.Writer, later, 31, periods.Day, 20); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day' AND start_day='2026-10-16'`) != 0 {
		t.Fatal("Shanghai daily cutoff wrong")
	}
	if _, err := reports.Prune(ctx, db.Writer, now, 31, periods.Month, 20); err == nil {
		t.Fatal("monthly cleanup accepted")
	}
}

func TestRetentionYearBoundaryAndConfigurableDays(t *testing.T) {
	db := reportDB(t)
	id := createReportClient(t, db)
	for _, day := range []string{"2026-12-30", "2026-12-31", "2027-01-01"} {
		periodFixture(t, db, periods.Day, day, id, 0, 0)
	}
	for _, day := range []string{"2026-12-21", "2026-12-28", "2027-01-04"} {
		periodFixture(t, db, periods.Week, day, id, 0, 0)
	}
	ctx := context.Background()
	now := instant("2027-01-03T16:00:00Z")
	if _, err := reports.Prune(ctx, db.Writer, now, 5, periods.Day, 20); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day' AND start_day='2026-12-30'`) != 0 || scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='day'`) != 2 {
		t.Fatal("year daily boundary")
	}
	if _, err := reports.Prune(ctx, db.Writer, now, 5, periods.Week, 20); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week' AND start_day='2026-12-21'`) != 0 || scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='week'`) != 2 {
		t.Fatal("year weekly boundary")
	}
}

func TestRetentionRollbackAndExternalLockCancellation(t *testing.T) {
	db := reportDB(t)
	id := createReportClient(t, db)
	periodFixture(t, db, periods.Day, "2026-01-01", id, 0, 0)
	ctx := context.Background()
	if _, err := db.Writer.Exec(`CREATE TRIGGER fail BEFORE DELETE ON period_stats BEGIN SELECT RAISE(ABORT,'delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Prune(ctx, db.Writer, instant("2026-11-01T00:00:00Z"), 31, periods.Day, 1); err == nil {
		t.Fatal("trigger ignored")
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats`) != 1 {
		t.Fatal("failed retention deleted row")
	}
	if _, err := db.Writer.Exec(`DROP TRIGGER fail`); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := db.Reader.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	lock, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer lock.Exec(`ROLLBACK`)
	limited, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := reports.Prune(limited, db.Writer, instant("2026-11-01T00:00:00Z"), 31, periods.Day, 1); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("retention deadline: %v", err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats`) != 1 || scalar(t, db.Writer, `PRAGMA busy_timeout`) != 5000 {
		t.Fatal("locked retention lost row or timeout")
	}
	w, err := reports.NewWriter(processorFunc(func(context.Context, int64, string, reports.Prepared) (int64, error) { return 0, nil }), reports.WriterOptions{QueueCapacity: 1, TransactionTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	result, err := w.EnqueueMaintenance(ctx, func(ctx context.Context) error {
		close(started)
		_, err := reports.Prune(ctx, db.Writer, instant("2026-11-01T00:00:00Z"), 31, periods.Day, 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	shutdown, cancelShutdown := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShutdown()
	start = time.Now()
	if err := w.Close(shutdown); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("retention cancel: %v", err)
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats`) != 1 || scalar(t, db.Writer, `PRAGMA busy_timeout`) != 5000 {
		t.Fatal("cancelled retention changed row or timeout")
	}
}

func TestMaintenanceSchedulesRetention(t *testing.T) {
	db := reportDB(t)
	id := createReportClient(t, db)
	old := time.Now().UTC().AddDate(0, 0, -90).Format("2006-01-02")
	for _, kind := range []string{periods.Day, periods.Week} {
		periodFixture(t, db, kind, old, id, 0, 0)
	}
	periodFixture(t, db, periods.Month, "2020-01-01", id, 0, 0)
	opts := reports.DefaultMaintenanceOptions()
	opts.Interval = 20 * time.Millisecond
	opts.ObservationInterval = 100 * time.Millisecond
	opts.BatchSize = 1
	_, _, _ = startMaintenance(t, db, opts, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), processorFunc(func(context.Context, int64, string, reports.Prepared) (int64, error) { return 0, nil }))
	waitFor(t, func() bool {
		return scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind IN ('day','week')`) == 0
	})
	if scalar(t, db.Reader, `SELECT count(*) FROM period_stats WHERE kind='month'`) != 1 {
		t.Fatal("monthly row lost")
	}
}
