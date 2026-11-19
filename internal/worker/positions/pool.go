package positions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/fancyqqq/tripgo-avito/internal/model"
)

var (
	ErrStopped        = errors.New("position worker pool stopped")
	ErrAlreadyStarted = errors.New("position worker pool already started")
)

type EnqueueResult string

const (
	Queued    EnqueueResult = "queued"
	Duplicate EnqueueResult = "duplicate"
	Dropped   EnqueueResult = "dropped"
)

type PoolConfig struct {
	Workers        int
	QueueSize      int
	RequestTimeout time.Duration
}

type Snapshot struct {
	QueueSize     int
	ActiveWorkers int
	Pending       int
}

type task struct {
	trip    model.StaleTrip
	origin  trace.SpanContext
	running bool
}

type Pool struct {
	config  PoolConfig
	gateway pushGateway
	metrics requestMetrics
	tracer  trace.Tracer
	logger  *slog.Logger
	queue   chan *task
	pending map[uuid.UUID]*task
	mu      sync.Mutex
	active  int
	started bool
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	done    chan struct{}
}

func NewPool(cfg PoolConfig, gateway pushGateway, metrics requestMetrics, tracer trace.Tracer, logger *slog.Logger) (*Pool, error) {
	if cfg.Workers <= 0 || cfg.QueueSize <= 0 || cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("position pool workers, queue size and request timeout must be positive")
	}
	if gateway == nil || metrics == nil || tracer == nil || logger == nil {
		return nil, fmt.Errorf("position pool dependencies must not be nil")
	}
	return &Pool{
		config: cfg, gateway: gateway, metrics: metrics, tracer: tracer, logger: logger,
		queue: make(chan *task, cfg.QueueSize), pending: make(map[uuid.UUID]*task), done: make(chan struct{}),
	}, nil
}

func (p *Pool) Enqueue(ctx context.Context, trip model.StaleTrip) (EnqueueResult, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.mu.Lock()
	if p.closed || (p.ctx != nil && p.ctx.Err() != nil) {
		p.mu.Unlock()
		return "", ErrStopped
	}
	if _, exists := p.pending[trip.TripID]; exists {
		p.mu.Unlock()
		return Duplicate, nil
	}
	queued := &task{trip: trip, origin: trace.SpanContextFromContext(ctx)}
	p.pending[trip.TripID] = queued
	select {
	case p.queue <- queued:
		p.mu.Unlock()
		return Queued, nil
	default:
		delete(p.pending, trip.TripID)
		p.mu.Unlock()
		p.logger.WarnContext(ctx, "position request queue full", "trip_id", trip.TripID, "queue_size", p.config.QueueSize)
		p.metrics.RequestDropped(ctx)
		return Dropped, nil
	}
}

func (p *Pool) Cancel(id uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if queued, exists := p.pending[id]; exists && !queued.running {
		delete(p.pending, id)
	}
}

func (p *Pool) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{QueueSize: len(p.queue), ActiveWorkers: p.active, Pending: len(p.pending)}
}

func (p *Pool) Done() <-chan struct{} {
	return p.done
}
