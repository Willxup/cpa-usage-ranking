package test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
)

func TestSettlementHonorsDeadlineUnderExternalLock(t *testing.T) {
	db := reportDB(t)
	registry := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{}}`)
	id := createReportClient(t, db)
	store := reports.NewStore(db.Writer, registry, func() time.Time { return instant("2026-11-01T12:00:00Z") })
	if _, err := store.Apply(context.Background(), id, "2026-11-01", prepared(t, 1, usage("alpha", 3, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := reports.AdvanceWindow(context.Background(), db.Writer, instant("2026-11-03T00:00:00Z")); err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	ok, err := reports.Finalize(ctx, db.Writer, "2026-11-01", id)
	if ok || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("deadline: %v %v after %v", ok, err, time.Since(start))
	}
	if scalar(t, db.Reader, `SELECT count(*) FROM daily_reports`) != 1 || scalar(t, db.Reader, `SELECT count(*) FROM period_stats`) != 0 {
		t.Fatal("locked settlement changed data")
	}
	if scalar(t, db.Writer, `PRAGMA busy_timeout`) != 5000 {
		t.Fatal("busy timeout not restored")
	}
}
