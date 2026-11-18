package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"

	"github.com/fancyqqq/tripgo-avito/internal/config"
	"github.com/fancyqqq/tripgo-avito/internal/container"
	"github.com/fancyqqq/tripgo-avito/internal/infra/logging"
	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
)

func main() {
	if err := run(); err != nil {
		service := os.Getenv("OTEL_SERVICE_NAME")
		if service == "" {
			service = "trip-service"
		}
		logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		logger.Error("service failed", "service", service, "trace_id", "", "span_id", "", "error", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
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

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	ctx, stop := signal.NotifyContext(appCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := telemetry.Setup(appCtx)
	if err != nil {
		return fmt.Errorf("init telemetry: %w", err)
	}
	var c *container.Container
	var serverDone chan struct{}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if c != nil {
			runErr = errors.Join(runErr, shutdown(shutdownCtx, c.HTTPServer, c.PositionScanner, c.PositionWorkers, c.Close))
			if serverDone != nil {
				<-serverDone
			}
		}
		flushTelemetry(shutdownCtx, logger, shutdownTelemetry)
		if c != nil {
			logger.Info("service stopped")
		}
	}()

	c, err = container.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if err := c.PositionWorkers.Start(appCtx); err != nil {
		return fmt.Errorf("start position workers: %w", err)
	}
	if err := c.PositionScanner.Start(appCtx); err != nil {
		return fmt.Errorf("start position scanner: %w", err)
	}

	serverErr := make(chan error, 1)
	serverDone = make(chan struct{})
	go func() {
		defer close(serverDone)
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

	return nil
}

func flushTelemetry(ctx context.Context, logger *slog.Logger, shutdown func(context.Context) error) {
	if err := shutdown(ctx); err != nil {
		logger.Warn("telemetry shutdown failed", "error", err)
	}
}
