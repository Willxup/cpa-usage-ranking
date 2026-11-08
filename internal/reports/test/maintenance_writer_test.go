package test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
)

func TestWriterAlternatesReportsAndMaintenance(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var order []string
	record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	processor := processorFunc(func(ctx context.Context, id int64, _ string, _ reports.Prepared) (int64, error) {
		record("report")
		if id == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return id, nil
	})
	w, err := reports.NewWriter(processor, reports.WriterOptions{QueueCapacity: 3, TransactionTimeout: time.Second * 5})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(context.Background())
	first, err := w.Enqueue(context.Background(), 1, "", reports.Prepared{})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := w.Enqueue(context.Background(), 2, "", reports.Prepared{})
	if err != nil {
		t.Fatal(err)
	}
	third, err := w.Enqueue(context.Background(), 3, "", reports.Prepared{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := w.EnqueueMaintenance(context.Background(), func(context.Context) error { record("maintenance"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.EnqueueMaintenance(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, reports.ErrQueueFull) {
		t.Fatalf("maintenance queue unbounded: %v", err)
	}
	close(release)
	for _, r := range []<-chan reports.WriteResult{first, second, third} {
		if got := resultWithin(t, r); got.Err != nil {
			t.Fatal(got.Err)
		}
	}
	if err := <-m; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"report", "maintenance", "report", "report"}) {
		t.Fatalf("ordering %v", got)
	}
}

func TestWriterGivesReportTurnAfterMaintenance(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var order []string
	record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	w, err := reports.NewWriter(processorFunc(func(context.Context, int64, string, reports.Prepared) (int64, error) { record("report"); return 1, nil }), reports.WriterOptions{QueueCapacity: 1, TransactionTimeout: time.Second * 5})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(context.Background())
	first, err := w.EnqueueMaintenance(context.Background(), func(ctx context.Context) error {
		record("maintenance")
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := w.EnqueueMaintenance(context.Background(), func(context.Context) error { record("maintenance"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.Enqueue(context.Background(), 1, "", reports.Prepared{})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if r := resultWithin(t, report); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"maintenance", "report", "maintenance"}) {
		t.Fatalf("ordering %v", got)
	}
}

func TestWriterCancelsMaintenanceOnShutdown(t *testing.T) {
	started := make(chan struct{})
	w, err := reports.NewWriter(processorFunc(func(context.Context, int64, string, reports.Prepared) (int64, error) { return 0, nil }), reports.WriterOptions{QueueCapacity: 1, TransactionTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.EnqueueMaintenance(context.Background(), func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("transaction: %v", err)
	}
}
