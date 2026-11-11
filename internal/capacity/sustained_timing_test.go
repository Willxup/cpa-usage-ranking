package capacity

import (
	"bufio"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestSustainedArrivalsAccountedBeforeBoundary(t *testing.T) {
	start := time.Unix(1000, 0)
	s := &sustainedState{rate: 10, nextArrival: start.Add(100 * time.Millisecond)}
	var planned []int64
	draw := func(rate float64) time.Duration {
		if rate != 10 {
			t.Fatalf("arrival used the new stage rate: %v", rate)
		}
		return 100 * time.Millisecond
	}
	create := func(source string, at int64) error {
		if source != "arrival" {
			t.Fatalf("unexpected source %q", source)
		}
		planned = append(planned, at)
		return nil
	}
	if err := sustainedArrivalsThrough(s, start.Add(350*time.Millisecond), draw, create); err != nil {
		t.Fatal(err)
	}
	want := []int64{start.Add(100 * time.Millisecond).UnixNano(), start.Add(200 * time.Millisecond).UnixNano(), start.Add(300 * time.Millisecond).UnixNano()}
	if !reflect.DeepEqual(planned, want) || !s.nextArrival.Equal(start.Add(400*time.Millisecond)) {
		t.Fatalf("due arrivals lost: got=%v next=%v", planned, s.nextArrival)
	}
	// A paused stage cannot silently create more arrivals.
	s.rate = 0
	if err := sustainedArrivalsThrough(s, start.Add(time.Second), draw, create); err != nil || len(planned) != len(want) {
		t.Fatalf("paused clock generated work: %v, %v", planned, err)
	}
}

func TestSustainedJournalFailureIsSticky(t *testing.T) {
	j := &sustainedJournal{out: bufio.NewWriterSize(failingWriter{}, 16)}
	if err := j.write(sustainedRecord{Kind: "a record longer than the buffer"}); err == nil {
		t.Fatal("expected evidence write failure")
	}
	if err := j.write(sustainedRecord{Kind: "end"}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("journal accepted end after failure: %v", err)
	}
	if err := j.flush(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("journal flush hid failure: %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
