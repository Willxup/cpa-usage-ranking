package test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/storage"
)

func TestWindowCheckpointHonorsCancellationUnderExternalLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.db")
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	if err := reports.AdvanceWindow(context.Background(), db.Writer, instant("2026-11-01T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	lock, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer lock.Exec("ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = reports.AdvanceWindow(ctx, db.Writer, instant("2026-11-02T12:00:00Z"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("checkpoint error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("checkpoint ignored cancellation for %v", elapsed)
	}
	var watermark string
	if err := db.Reader.QueryRow("SELECT closed_before_day FROM report_window WHERE id=1").Scan(&watermark); err != nil {
		t.Fatal(err)
	}
	if watermark != "2026-10-31" {
		t.Fatalf("failed checkpoint changed watermark: %s", watermark)
	}
	if got := scalar(t, db.Writer, "PRAGMA busy_timeout"); got != 5000 {
		t.Fatalf("busy timeout not restored: %d", got)
	}
}
