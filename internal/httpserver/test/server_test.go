package test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"cpa-usage-ranking/internal/httpserver"
)

type observedListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *observedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}

func listen(t *testing.T) *observedListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return &observedListener{Listener: listener, closed: make(chan struct{})}
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server behavior")
		var zero T
		return zero
	}
}

type response struct {
	status int
	body   string
	err    error
}

func get(url string) <-chan response {
	result := make(chan response, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		res, err := client.Get(url)
		if err != nil {
			result <- response{err: err}
			return
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		result <- response{status: res.StatusCode, body: string(body), err: err}
	}()
	return result
}

func TestServerHandlesRequestAndStops(t *testing.T) {
	listener := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- httpserver.Serve(ctx, listener, httpserver.NewRouter(quietLogger()), time.Second) }()
	res := receive(t, get("http://"+listener.Addr().String()+"/healthz"))
	if res.err != nil || res.status != 200 {
		t.Fatalf("health request = %+v", res)
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("server continued accepting after shutdown")
	}
}

func TestShutdownDrainsInFlightRequest(t *testing.T) {
	listener := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "completed")
	})
	done := make(chan error, 1)
	go func() { done <- httpserver.Serve(ctx, listener, handler, time.Second) }()
	request := get("http://" + listener.Addr().String())
	receive(t, started)
	cancel()
	receive(t, listener.closed)
	select {
	case err := <-done:
		t.Fatalf("server stopped before draining request: %v", err)
	default:
	}
	finish()
	res := receive(t, request)
	if res.err != nil || res.body != "completed" {
		t.Fatalf("in-flight request = %+v", res)
	}
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownTimeoutClosesActiveConnection(t *testing.T) {
	listener := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, exited := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(exited)
	})
	done := make(chan error, 1)
	go func() { done <- httpserver.Serve(ctx, listener, handler, 30*time.Millisecond) }()
	request := get("http://" + listener.Addr().String())
	receive(t, started)
	cancel()
	if err := receive(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline, got %v", err)
	}
	receive(t, exited)
	if res := receive(t, request); res.err == nil {
		t.Fatalf("incomplete response reported as success: %+v", res)
	}
}

func TestClosedListenerReturnsServeError(t *testing.T) {
	listener := listen(t)
	_ = listener.Close()
	err := httpserver.Serve(context.Background(), listener, http.NotFoundHandler(), time.Second)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected closed-listener error, got %v", err)
	}
}

func TestAlreadyCancelledContextClosesListener(t *testing.T) {
	listener := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := httpserver.Serve(ctx, listener, http.NotFoundHandler(), time.Second); err != nil {
		t.Fatal(err)
	}
	receive(t, listener.closed)
}
