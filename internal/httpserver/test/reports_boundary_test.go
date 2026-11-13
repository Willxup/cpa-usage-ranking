package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
)

// Authentication has already succeeded when the processor signals; the
// database writer lock keeps the report queued until the test changes state.
func TestReportHTTPRechecksDeadlineAndDisableAfterAuthentication(t *testing.T) {
	started := make(chan struct{}, 2)
	f := reportSetup(t, defaultRegistrationSettings(), generousReportLimits(), func(store *reports.Store) reports.SnapshotProcessor {
		return httpProcessor(func(ctx context.Context, id int64, day string, p reports.Prepared) (int64, error) {
			started <- struct{}{}
			return store.Apply(ctx, id, day, p)
		})
	})
	client := f.seed(t, testSecret(1))
	f.clock.Store(time.Date(2026, 10, 31, 15, 59, 59, 0, time.UTC).UnixNano())
	tx, err := f.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan registrationResponse, 1)
	go func() { result <- f.report(t, "2026-10-30", testSecret(1), reportJSON, nil) }()
	<-started
	f.clock.Store(time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC).UnixNano())
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	requireHTTPError(t, <-result, 410, "expired_day")

	tx, err = f.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE clients SET disabled=1 WHERE id=?`, client.ID); err != nil {
		t.Fatal(err)
	}
	go func() { result <- f.report(t, "2026-10-31", testSecret(1), reportJSON, nil) }()
	<-started
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	requireHTTPError(t, <-result, 403, "client_disabled")
	var count int
	if err := f.db.Reader.QueryRow(`SELECT COUNT(*) FROM daily_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected report persisted: %d %v", count, err)
	}
}
