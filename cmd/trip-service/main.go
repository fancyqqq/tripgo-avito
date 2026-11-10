package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/fancyqqq/tripgo-avito/internal/config"
	"github.com/fancyqqq/tripgo-avito/internal/container"
	"github.com/fancyqqq/tripgo-avito/internal/infra/logging"
	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
)

const telemetryShutdownTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := logging.New(os.Stdout, cfg.LogLevel, cfg.ServiceName)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("opentelemetry error", "error", err)
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := telemetry.Setup(ctx)
	if err != nil {
		return fmt.Errorf("init telemetry: %w", err)
	}
	defer flushTelemetry(logger, shutdownTelemetry)

	c, err := container.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer c.Close()

	serverErr := make(chan error, 1)
	go func() {
		if err := c.HTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	logger.InfoContext(ctx, "service started", "addr", cfg.HTTP.Addr)

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := c.HTTPServer.Shutdown(shutdownCtx); err != nil {
		_ = c.HTTPServer.Close()
		return fmt.Errorf("graceful shutdown timed out, forced close: %w", err)
	}

	logger.Info("service stopped")
	return nil
}

func flushTelemetry(logger *slog.Logger, shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
	defer cancel()

	if err := shutdown(ctx); err != nil {
		logger.Warn("telemetry shutdown failed", "error", err)
	}
}
