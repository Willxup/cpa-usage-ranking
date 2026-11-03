package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Serve drains active requests on cancellation, then force-closes on timeout.
// It owns listener and returns only after the serving goroutine has stopped.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, shutdownTimeout time.Duration) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	defer server.Close()
	if ctx.Err() != nil {
		return listener.Close()
	}

	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		return servingError(err)
	case <-ctx.Done():
		// 关停等待使用独立期限，不能沿用已取消的运行上下文。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			_ = server.Close()
		}
		serveErr := <-result
		if err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return servingError(serveErr)
	}
}

func servingError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve HTTP: %w", err)
}
