package capacity

import (
	"net/http"
	"testing"
	"time"
)

func TestInitialWriteSpanRequiresEveryFirstAttempt(t *testing.T) {
	s := BurstSummary{Clients: 2, ReleasedUnixNS: 1000000000, BurstSample: BurstSample{
		InitialFirstAttemptsWritten: 1,
		FirstInitialWriteUnixNS:     1100000000,
		LastInitialWriteUnixNS:      1600000000,
	}}
	setInitialWriteSpans(&s)
	if s.InitialWriteSpanMS != nil || s.ObservedInitialWriteSpanMS == nil || *s.ObservedInitialWriteSpanMS != 500 {
		t.Fatalf("partial sample was mistaken for complete wave: %+v", s)
	}
	s.InitialFirstAttemptsWritten = 2
	setInitialWriteSpans(&s)
	if s.InitialWriteSpanMS == nil || *s.InitialWriteSpanMS != 500 || s.LastInitialWriteFromReleaseMS == nil || *s.LastInitialWriteFromReleaseMS != 600 {
		t.Fatalf("complete wave span: %+v", s)
	}
}

func TestRetryDelayHonorsRetryAfterAndBound(t *testing.T) {
	delay, err := burstRetryDelay("1", 13, 2, 1, time.Second, 3*time.Second)
	if err != nil || delay < time.Second || delay > 2*time.Second {
		t.Fatalf("retry-after/jitter: %s %v", delay, err)
	}
	if _, err := burstRetryDelay("4", 13, 2, 1, time.Second, 3*time.Second); err == nil {
		t.Fatal("retry-after exceeding the bound was silently shortened")
	}
	date := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if delay, err = burstRetryDelay(date, 13, 2, 1, 0, 3*time.Second); err != nil || delay < time.Second || delay > 3*time.Second {
		t.Fatalf("HTTP-date retry-after: %s %v", delay, err)
	}
}
