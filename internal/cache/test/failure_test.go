package test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-ranking/internal/cache"
	"cpa-usage-ranking/internal/periods"
	"cpa-usage-ranking/internal/statistics"
)

func TestFirstFailureThenSuccessAndCombinedCapacity(t *testing.T) {
	_, r := cacheFixture(t)
	var calls atomic.Int32
	var allow atomic.Bool
	item := cache.Item{ID: "first", Kind: periods.All, Encode: func(_ context.Context, _ statistics.Snapshot, w io.Writer) error {
		calls.Add(1)
		if !allow.Load() {
			return errors.New("failure")
		}
		_, err := io.WriteString(w, "ok")
		return err
	}}
	c := newCache(t, r, []cache.Item{item}, 30*time.Millisecond)
	_, _ = run(t, c)
	waitFor(t, func() bool { return calls.Load() > 0 })
	if _, err := c.Current(); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatal(err)
	}
	allow.Store(true)
	waitFor(t, func() bool { _, err := c.Current(); return err == nil })
	opts := cache.DefaultOptions()
	opts.Interval = 30 * time.Millisecond
	opts.MaxBytes = 3
	always := func(_ context.Context, _ statistics.Snapshot, w io.Writer) error {
		_, err := io.WriteString(w, "ab")
		return err
	}
	full, err := cache.New(r, []cache.Item{{ID: "a", Kind: periods.All, Encode: always}, {ID: "b", Kind: periods.All, Encode: always}}, opts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = run(t, full)
	time.Sleep(65 * time.Millisecond)
	if _, err := full.Current(); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatal("oversized round published", err)
	}
}
func TestCancellationDuringBuild(t *testing.T) {
	_, r := cacheFixture(t)
	entered := make(chan struct{})
	item := cache.Item{ID: "blocked", Kind: periods.All, Encode: func(ctx context.Context, _ statistics.Snapshot, _ io.Writer) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	c := newCache(t, r, []cache.Item{item}, time.Hour)
	cancel, done := run(t, c)
	receive(t, entered)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop build")
	}
	if _, err := c.Current(); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatal(err)
	}
}
