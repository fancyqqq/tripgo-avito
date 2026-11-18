package positions

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ScannerConfig struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
}

type Scanner struct {
	config    ScannerConfig
	trips     staleTrips
	queue     positionQueue
	metrics   scanMetrics
	tracer    trace.Tracer
	logger    *slog.Logger
	newTicker func(time.Duration) Ticker
	mu        sync.Mutex
	started   bool
	closed    bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewScanner(cfg ScannerConfig, trips staleTrips, queue positionQueue, metrics scanMetrics, tracer trace.Tracer, logger *slog.Logger, newTicker func(time.Duration) Ticker) (*Scanner, error) {
	if cfg.CheckInterval <= 0 || cfg.StaleAfter <= 0 {
		return nil, fmt.Errorf("position scan interval and stale threshold must be positive")
	}
	if trips == nil || queue == nil || metrics == nil || tracer == nil || logger == nil || newTicker == nil {
		return nil, fmt.Errorf("position scanner dependencies must not be nil")
	}
	return &Scanner{
		config: cfg, trips: trips, queue: queue, metrics: metrics, tracer: tracer, logger: logger,
		newTicker: newTicker, done: make(chan struct{}),
	}, nil
}

func (s *Scanner) run(ctx context.Context, ticker Ticker) {
	s.logger.InfoContext(ctx, "position scanner started", "check_interval", s.config.CheckInterval, "stale_after", s.config.StaleAfter)
	defer s.finish(ctx, ticker)
	ticks := ticker.C()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok || ctx.Err() != nil {
				return
			}
			s.scan(ctx)
		}
	}
}

func (s *Scanner) scan(ctx context.Context) {
	ctx, span := s.tracer.Start(ctx, "positions.scan")
	defer span.End()
	trips, err := s.trips.ListStale(ctx, s.config.StaleAfter)
	if err != nil {
		s.fail(ctx, span, "stale trip scan failed", err)
		return
	}
	span.SetAttributes(attribute.Int("trips.count", len(trips)))
	for _, trip := range trips {
		if ctx.Err() != nil {
			return
		}
		result, err := s.queue.Enqueue(ctx, trip)
		if err != nil {
			s.fail(ctx, span, "enqueue stale trip failed", err)
			return
		}
		s.metrics.StaleChecked(ctx, string(result))
	}
}

func (s *Scanner) fail(ctx context.Context, span trace.Span, message string, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	if ctx.Err() == nil {
		s.logger.WarnContext(ctx, message, "error", err)
	}
}
