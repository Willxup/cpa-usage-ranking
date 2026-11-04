package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"cpa-usage-ranking/internal/config"
	"cpa-usage-ranking/internal/httpserver"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("service stopped")
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	router := httpserver.NewRouter(logger)
	if cfg.WebDir != "" {
		frontend, err := httpserver.OpenFrontend(cfg.WebDir)
		if err != nil {
			return err
		}
		defer frontend.Close()
		frontend.Mount(router)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	logger.Info("http listener opened", "address", listener.Addr().String())
	return httpserver.Serve(ctx, listener, router, cfg.ShutdownTimeout)
}
