package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/config"
	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/registration"
	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/storage"
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

func run(logger *slog.Logger) (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	modelCatalog, err := models.Load(cfg.ModelsPath)
	if err != nil {
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	router := httpserver.NewRouter(logger)
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return fmt.Errorf("configure trusted proxies: %w", err)
	}
	avatarFiles := avatars.BuiltinFS()
	catalog, err := avatars.Load(avatarFiles)
	if err != nil {
		return err
	}
	if err := httpserver.MountAvatars(router, catalog, avatarFiles); err != nil {
		return err
	}
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
	db, err := storage.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	// Serve 返回前已停止接入并排空请求，再释放数据库连接。
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := storage.MigrateSchema(ctx, db.Writer); err != nil {
		return err
	}
	if err := reports.AdvanceWindow(ctx, db.Writer, time.Now()); err != nil {
		return fmt.Errorf("advance report window: %w", err)
	}
	modelRegistry, err := models.Register(ctx, db.Writer, modelCatalog)
	if err != nil {
		return err
	}
	logger.Info("sqlite initialized")
	repository := clients.NewRepository(db)
	reportAdmission, err := reports.NewAdmission(cfg.ReportIngestion, nil)
	if err != nil {
		return err
	}
	reportWriter, err := reports.NewWriter(reports.NewStore(db.Writer, modelRegistry, nil), cfg.ReportWriter)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		err = errors.Join(err, reportWriter.Close(shutdown))
	}()
	windowCtx, stopWindow := context.WithCancel(context.Background())
	windowDone := make(chan struct{})
	go func() { defer close(windowDone); watchReportWindow(windowCtx, db.Writer, logger) }()
	defer func() { stopWindow(); <-windowDone }()
	if err := httpserver.MountReports(router, auth.NewAuthenticator(repository), reportAdmission, reportWriter, int64(cfg.ReportIngestion.MaxBodyBytes)); err != nil {
		return err
	}
	registrationService, err := registration.NewService(repository, catalog, registration.Options{
		CreationLimits: cfg.RegistrationCreations, NewIdentitiesEnabled: cfg.RegistrationEnabled,
	})
	if err != nil {
		return err
	}
	requestLimiter, err := registration.NewRequestLimiter(cfg.RegistrationRequests, nil)
	if err != nil {
		return err
	}
	if err := httpserver.MountRegistration(router, registrationService, requestLimiter, httpserver.RegistrationHTTPOptions{
		GroupIPv6By64: cfg.RegistrationIPv6By64, MaxBodyBytes: int64(cfg.RegistrationMaxBodyBytes),
	}); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	logger.Info("http listener opened", "address", listener.Addr().String())
	return httpserver.Serve(ctx, listener, router, cfg.ShutdownTimeout)
}

// Observe midnight even without incoming reports. A one-minute maximum wait
// also notices forward clock corrections; every report independently advances
// and rechecks the durable watermark before it can commit.
func watchReportWindow(ctx context.Context, writer *sql.DB, logger *slog.Logger) {
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		delay := next.Sub(now)
		if delay > time.Minute {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := reports.AdvanceWindow(ctx, writer, time.Now()); err != nil && ctx.Err() == nil {
				logger.Error("report window checkpoint failed")
			}
		}
	}
}
