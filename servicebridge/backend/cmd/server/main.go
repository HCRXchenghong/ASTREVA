package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/httpx"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

func main() {
	cfg := config.Load()
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	if err := cfg.Validate(); err != nil {
		logger.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}

	storeOptions := store.Options{
		Logger:                 logger,
		OpenAIAPIKey:           cfg.OpenAIAPIKey,
		OpenAIBaseURL:          cfg.OpenAIBaseURL,
		OpenAIModel:            cfg.OpenAIModel,
		OpenAIAPIType:          cfg.OpenAIAPIType,
		DataEncryptionKey:      cfg.DataEncryptionKey,
		BootstrapAdminPassword: cfg.BootstrapAdminPassword,
		BootstrapAgentPassword: cfg.BootstrapAgentPassword,
		FCMAccessToken:         cfg.FCMAccessToken,
	}
	dataStore, closeStore, err := buildStore(startupCtx, cfg, storeOptions, logger)
	if err != nil {
		logger.Error("store initialization failed", "error", err)
		os.Exit(1)
	}
	defer closeStore()

	hub := realtime.NewHubWithNodeID(cfg.NodeID)
	if strings.TrimSpace(cfg.RedisAddr) != "" {
		bus, err := realtime.NewRedisBus(startupCtx, cfg.RedisAddr, cfg.RedisChannel, cfg.NodeID, logger, hub)
		if err != nil {
			logger.Error("redis event bus initialization failed", "error", err)
			os.Exit(1)
		}
		hub.SetPublisher(bus.Publish)
		defer bus.Close()
		logger.Info("redis event bus enabled", "addr", cfg.RedisAddr, "channel", cfg.RedisChannel)
	}
	service := app.NewService(dataStore, hub, logger)
	service.SetNotifier(buildNotifier(cfg, logger, dataStore))
	// SLA 巡检（首响超时 / 待回复回退 / 静默自动关闭）：间隔取 SERVICE_POLICY_TICK_SECONDS，
	// 默认 30 秒、最小 5 秒；进程退出时优雅停止。
	policyTick := app.PolicyTickInterval()
	service.StartPolicyLoop(policyTick)
	logger.Info("service policy sweep loop started", "interval", policyTick.String())
	mux := httpx.NewRouter(cfg, logger, service)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service.StopPolicyLoop()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func buildStore(ctx context.Context, cfg config.Config, options store.Options, logger *slog.Logger) (store.Store, func(), error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.StoreDriver))
	if driver == "" {
		driver = "memory"
	}
	if driver == "postgres" || (driver == "memory" && strings.TrimSpace(cfg.DatabaseURL) != "") {
		postgresStore, err := store.NewPostgresStore(ctx, cfg.DatabaseURL, options)
		if err != nil {
			return nil, func() {}, err
		}
		logger.Info("store initialized", "driver", "postgres", "database_configured", true)
		return postgresStore, func() { postgresStore.Close() }, nil
	}
	memoryStore := store.NewMemoryStore(options)
	logger.Info("store initialized", "driver", "memory", "database_configured", cfg.DatabaseURL != "")
	return memoryStore, func() {}, nil
}

// buildNotifier 返回按后台配置发送的推送通道；未配置任何通道时只记录日志。
// FCMEndpoint 保留为兼容旧部署的配置项，实际端点由服务账号/项目号推导。
// email 提醒渠道的 SMTP 参数每次都从 alert_smtp_settings 单例读取，管理员改完立即生效。
func buildNotifier(cfg config.Config, logger *slog.Logger, dataStore store.Store) notify.Notifier {
	provider := notify.NewProvider(logger)
	provider.SetSMTPProvider(dataStore.AlertSMTPSettings)
	env := strings.ToLower(strings.TrimSpace(cfg.Env))
	provider.SetAllowPrivate(env != "prod" && env != "production")
	return provider
}
