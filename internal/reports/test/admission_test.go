package test

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-ranking/internal/reports"
)

func TestReportClientRateCapacityExpiryAndClockRollback(t *testing.T) {
	limits := reports.DefaultIngestionLimits()
	limits.MaxClients = 1
	limits.GlobalBurst = 100
	now := instant("2026-11-01T12:00:00Z")
	a, err := reports.NewAdmission(limits, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Admit(1); err != nil {
		t.Fatal(err)
	}
	if err := a.Admit(1); err != nil {
		t.Fatal(err)
	}
	var limited *reports.AdmissionError
	if err := a.Admit(1); !errors.As(err, &limited) || limited.RetryAfterSeconds() != 60 {
		t.Fatalf("client rate: %v", err)
	}
	if err := a.Admit(2); !errors.As(err, &limited) || limited.Code != "report_capacity" {
		t.Fatalf("active state was evicted: %v", err)
	}
	now = now.Add(-time.Hour)
	if err := a.Admit(1); !errors.As(err, &limited) || limited.RetryAfterSeconds() != 60 {
		t.Fatalf("clock rollback reset limit: %v", err)
	}
	now = now.Add(time.Hour + time.Minute)
	if err := a.Admit(1); err != nil {
		t.Fatalf("refill after minute: %v", err)
	}
	now = now.Add(11 * time.Minute)
	if err := a.Admit(2); err != nil {
		t.Fatalf("expired refilled state not reclaimed: %v", err)
	}
}

func TestGlobalAndClientLimitRetryAndConcurrency(t *testing.T) {
	limits := reports.DefaultIngestionLimits()
	limits.GlobalBurst = 2
	limits.GlobalInterval = 10 * time.Second
	now := instant("2026-11-01T12:00:00Z")
	a, err := reports.NewAdmission(limits, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Admit(1); err != nil {
		t.Fatal(err)
	}
	if err := a.Admit(1); err != nil {
		t.Fatal(err)
	}
	var limited *reports.AdmissionError
	if err := a.Admit(1); !errors.As(err, &limited) || limited.RetryAfterSeconds() != 60 {
		t.Fatalf("retry must cover both limits: %v", err)
	}
	if err := a.Admit(2); !errors.As(err, &limited) || limited.RetryAfterSeconds() != 10 {
		t.Fatalf("global limit: %v", err)
	}
	limits.GlobalBurst = 100
	b, err := reports.NewAdmission(limits, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Admit(7) == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 2 {
		t.Fatalf("concurrent burst allowed %d", allowed.Load())
	}
}

type blockedBody struct{ started, release chan struct{} }

func (b *blockedBody) Read(_ []byte) (int, error) { close(b.started); <-b.release; return 0, io.EOF }

func TestBodyItemAndDecodeConcurrencyLimits(t *testing.T) {
	limits := reports.DefaultIngestionLimits()
	limits.MaxBodyBytes = len(validPayload)
	limits.MaxItems = 1
	limits.ConcurrentDecodes = 1
	a, err := reports.NewAdmission(limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Decode(strings.NewReader(validPayload)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Decode(strings.NewReader(validPayload + " ")); !errors.Is(err, reports.ErrBodyTooLarge) {
		t.Fatalf("body limit: %v", err)
	}
	if _, err := reports.DecodeLimited([]byte(`{"schema_version":1,"revision":"0","items":[{},{}]}`), 1); !errors.Is(err, reports.ErrTooManyItems) {
		t.Fatalf("raw item limit: %v", err)
	}
	blocked := &blockedBody{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := a.Decode(blocked); done <- err }()
	<-blocked.started
	if _, err := a.Decode(strings.NewReader(validPayload)); !errors.Is(err, reports.ErrDecodeBusy) {
		t.Fatalf("decode concurrency: %v", err)
	}
	close(blocked.release)
	if err := <-done; !errors.Is(err, reports.ErrInvalid) {
		t.Fatalf("blocked body result: %v", err)
	}
	if _, err := a.Decode(strings.NewReader(validPayload)); err != nil {
		t.Fatalf("decode permit leaked: %v", err)
	}
}
