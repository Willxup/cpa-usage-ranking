package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/config"
	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/registration"
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
	modelRegistry, err := models.Register(ctx, db.Writer, modelCatalog)
	if err != nil {
		return err
	}
	// 当前还没有上报入口；保留启动快照至退出，后续接入时复用同一映射。
	defer runtime.KeepAlive(modelRegistry)
	logger.Info("sqlite initialized")
	registrationService, err := registration.NewService(clients.NewRepository(db), catalog, registration.Options{
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
