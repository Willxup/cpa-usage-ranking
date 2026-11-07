package reports

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrQueueFull    = errors.New("report write queue full")
	ErrWriterClosed = errors.New("report writer closed")
)

// SnapshotProcessor is intentionally limited to one report transaction.
type SnapshotProcessor interface {
	Apply(context.Context, int64, string, Prepared) (int64, error)
}

type WriterOptions struct {
	QueueCapacity      int
	TransactionTimeout time.Duration
}

func DefaultWriterOptions() WriterOptions {
	return WriterOptions{QueueCapacity: 128, TransactionTimeout: 5 * time.Second}
}

type WriteResult struct {
	Revision int64
	Err      error
}

type writeRequest struct {
	ctx      context.Context
	clientID int64
	day      string
	prepared Prepared
	result   chan WriteResult
}

// Writer has one worker and a bounded pending queue. The queue itself is never
// closed, so concurrent admission and shutdown cannot send on a closed channel.
type Writer struct {
	mu        sync.Mutex
	closed    bool
	queue     chan writeRequest
	stopping  chan struct{}
	done      chan struct{}
	abort     context.Context
	cancel    context.CancelFunc
	processor SnapshotProcessor
	timeout   time.Duration
}

func NewWriter(processor SnapshotProcessor, options WriterOptions) (*Writer, error) {
	if processor == nil || options.QueueCapacity <= 0 || options.TransactionTimeout <= 0 {
		return nil, fmt.Errorf("invalid report writer options")
	}
	abort, cancel := context.WithCancel(context.Background())
	w := &Writer{queue: make(chan writeRequest, options.QueueCapacity), stopping: make(chan struct{}), done: make(chan struct{}), abort: abort, cancel: cancel, processor: processor, timeout: options.TransactionTimeout}
	go w.run()
	return w, nil
}

// Enqueue returns immediately on admission or full queue. The result channel is
// buffered: a caller that disconnects never blocks the single worker.
func (w *Writer) Enqueue(ctx context.Context, clientID int64, day string, prepared Prepared) (<-chan WriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, ErrWriterClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req := writeRequest{ctx: ctx, clientID: clientID, day: day, prepared: prepared, result: make(chan WriteResult, 1)}
	select {
	case w.queue <- req:
		return req.result, nil
	default:
		return nil, ErrQueueFull
	}
}

func (w *Writer) run() {
	defer close(w.done)
	for {
		select {
		case req := <-w.queue:
			w.execute(req)
		case <-w.stopping:
			// Admission is already closed under the same lock as enqueue, so
			// the queue has a finite, stable set of accepted requests.
			for {
				select {
				case req := <-w.queue:
					w.execute(req)
				default:
					return
				}
			}
		}
	}
}

func (w *Writer) execute(req writeRequest) {
	ctx, cancel := context.WithTimeout(req.ctx, w.timeout)
	stop := context.AfterFunc(w.abort, cancel)
	defer func() { stop(); cancel() }()
	if w.abort.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		req.result <- WriteResult{Err: err}
		return
	}
	revision, err := w.processor.Apply(ctx, req.clientID, req.day, req.prepared)
	req.result <- WriteResult{Revision: revision, Err: err}
}

// Close stops admission, drains accepted work until ctx expires, then cancels
// unfinished transactions and waits for rollback/worker exit before returning.
// The database must be closed only after this returns.
func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.stopping)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
		w.cancel()
		return nil
	case <-ctx.Done():
		w.cancel()
		<-w.done
		return ctx.Err()
	}
}
