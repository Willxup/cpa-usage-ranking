package test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/storage"
)

func instant(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(err)
	}
	return t
}

func windowCheck(t *testing.T, db *storage.DB, day string, now time.Time) error {
	t.Helper()
	tx, err := db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return reports.CheckWindow(context.Background(), tx, day, now)
}

func TestUTCWindowAdvancesIndependentlyAndSurvivesRollbackRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "window.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := storage.MigrateSchema(ctx, db.Writer); err != nil {
		t.Fatal(err)
	}
	before := instant("2026-11-01T07:59:59+08:00")
	midnight := instant("2026-11-01T08:00:00+08:00")
	if err := reports.AdvanceWindow(ctx, db.Writer, before); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		day  string
		want error
	}{
		{"2026-10-31", nil}, {"2026-10-30", nil}, {"2026-10-29", reports.ErrExpiredDay},
		{"2026-11-01", reports.ErrFutureDay}, {"2026-2-01", reports.ErrInvalidDay},
		{"2026-02-29", reports.ErrInvalidDay}, {"2026-10-30T00:00:00Z", reports.ErrInvalidDay},
	} {
		if err := windowCheck(t, db, tc.day, before); !errors.Is(err, tc.want) {
			t.Fatalf("day %q: got %v want %v", tc.day, err, tc.want)
		}
	}
	if err := windowCheck(t, db, "2026-10-30", midnight); !errors.Is(err, reports.ErrWindowAdvance) {
		t.Fatalf("cross-midnight transaction skipped persistent advancement: %v", err)
	}
	// Advance before attempting the report; even an expired or invalid report
	// cannot undo the independently committed watermark.
	if err := reports.AdvanceWindow(ctx, db.Writer, midnight); err != nil {
		t.Fatal(err)
	}
	if err := windowCheck(t, db, "2026-10-30", midnight); !errors.Is(err, reports.ErrExpiredDay) {
		t.Fatal(err)
	}
	if err := windowCheck(t, db, "invalid", midnight); !errors.Is(err, reports.ErrInvalidDay) {
		t.Fatal(err)
	}
	if err := reports.AdvanceWindow(ctx, db.Writer, before); err != nil {
		t.Fatal(err)
	}
	if err := windowCheck(t, db, "2026-10-30", before); !errors.Is(err, reports.ErrExpiredDay) {
		t.Fatalf("clock rollback reopened day: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.MigrateSchema(ctx, db.Writer); err != nil {
		t.Fatal(err)
	}
	if err := reports.AdvanceWindow(ctx, db.Writer, before); err != nil {
		t.Fatal(err)
	}
	if err := windowCheck(t, db, "2026-10-30", before); !errors.Is(err, reports.ErrExpiredDay) {
		t.Fatalf("restart reopened day: %v", err)
	}
	if err := windowCheck(t, db, "2026-10-31", before); err != nil {
		t.Fatal(err)
	}
}

func TestWindowPersistenceFailureCannotAdmit(t *testing.T) {
	db := reportDB(t)
	ctx := context.Background()
	before, after := instant("2026-10-31T23:59:59Z"), instant("2026-11-01T00:00:00Z")
	if err := reports.AdvanceWindow(ctx, db.Writer, before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer.Exec(`CREATE TRIGGER block_window BEFORE UPDATE ON report_window BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := reports.AdvanceWindow(ctx, db.Writer, after); err == nil {
		t.Fatal("failed persistence was accepted")
	}
	if err := windowCheck(t, db, "2026-10-31", after); !errors.Is(err, reports.ErrWindowAdvance) {
		t.Fatalf("accepted without persistence: %v", err)
	}
}
