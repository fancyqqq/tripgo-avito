package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

const (
	success = "success"
	failed  = "error"
)

type fixture struct {
	pool    *positions.Pool
	gateway *MockpushGateway
	metrics *MockrequestMetrics
}

func poolConfig(workers, size int) positions.PoolConfig {
	return positions.PoolConfig{Workers: workers, QueueSize: size, RequestTimeout: 5 * time.Second}
}

func newFixture(t *testing.T, cfg positions.PoolConfig, tracer trace.Tracer) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{gateway: NewMockpushGateway(ctrl), metrics: NewMockrequestMetrics(ctrl)}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("positions-test")
	}
	var err error
	f.pool, err = positions.NewPool(cfg, f.gateway, f.metrics, tracer, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(f.pool.Close)
	return f
}

func candidate() model.StaleTrip {
	return model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastActivityAt: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)}
}

func enqueue(t *testing.T, pool *positions.Pool, trip model.StaleTrip, want positions.EnqueueResult) {
	t.Helper()
	result, err := pool.Enqueue(context.Background(), trip)
	if err != nil || result != want {
		t.Fatalf("Enqueue = %s, %v, want %s", result, err, want)
	}
}

func start(t *testing.T, pool *positions.Pool, ctx context.Context) {
	t.Helper()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func shutdown(t *testing.T, pool *positions.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if state := pool.Snapshot(); state != (positions.Snapshot{}) {
		t.Fatalf("state after shutdown = %+v", state)
	}
}

func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for synchronization signal")
	}
}

func waitRelease(ctx context.Context, release <-chan struct{}) error {
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPoolRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	for _, cfg := range []positions.PoolConfig{
		{Workers: 0, QueueSize: 1, RequestTimeout: time.Second},
		{Workers: 1, QueueSize: 0, RequestTimeout: time.Second},
		{Workers: 1, QueueSize: 1},
		{Workers: -1, QueueSize: 1, RequestTimeout: time.Second},
	} {
		ctrl := gomock.NewController(t)
		pool, err := positions.NewPool(cfg, NewMockpushGateway(ctrl), NewMockrequestMetrics(ctrl), noop.NewTracerProvider().Tracer("test"), slog.New(slog.DiscardHandler))
		if err == nil {
			pool.Close()
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	if pool, err := positions.NewPool(poolConfig(1, 1), nil, nil, nil, nil); err == nil {
		pool.Close()
		t.Fatal("nil dependencies accepted")
	}
}

func TestCancelledOldTaskCannotSendOrRemoveNewTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 2), nil)
	old := candidate()
	newTask := old
	newTask.DriverID = uuid.New()
	enqueue(t, f.pool, old, positions.Queued)
	enqueue(t, f.pool, old, positions.Duplicate)
	f.pool.Cancel(old.TripID)
	enqueue(t, f.pool, newTask, positions.Queued)
	started := make(chan struct{})
	release := make(chan struct{})
	f.gateway.EXPECT().RequestPosition(gomock.Any(), newTask.TripID, newTask.DriverID).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		close(started)
		return waitRelease(ctx, release)
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
	start(t, f.pool, context.Background())
	waitFor(t, started)
	enqueue(t, f.pool, newTask, positions.Duplicate)
	close(release)
	shutdown(t, f.pool)
}

func TestFullQueueDropsWithoutLeavingPendingEntry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 1), nil)
	first, second := candidate(), candidate()
	f.metrics.EXPECT().RequestDropped(gomock.Any())
	enqueue(t, f.pool, first, positions.Queued)
	enqueue(t, f.pool, second, positions.Dropped)
	if state := f.pool.Snapshot(); state.QueueSize != 1 || state.Pending != 1 {
		t.Fatalf("state after drop = %+v", state)
	}
	f.gateway.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).Return(nil)
	f.gateway.EXPECT().RequestPosition(gomock.Any(), second.TripID, second.DriverID).Return(nil)
	firstDone := make(chan struct{})
	gomock.InOrder(
		f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).Do(func(context.Context, string, time.Duration) { close(firstDone) }),
		f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()),
	)
	start(t, f.pool, context.Background())
	waitFor(t, firstDone)
	enqueue(t, f.pool, second, positions.Queued)
	shutdown(t, f.pool)
}

func TestRunningTripRemainsDeduplicated(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 1), nil)
	trip := candidate()
	started, release := make(chan struct{}), make(chan struct{})
	f.gateway.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		close(started)
		return waitRelease(ctx, release)
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
	start(t, f.pool, context.Background())
	enqueue(t, f.pool, trip, positions.Queued)
	waitFor(t, started)
	enqueue(t, f.pool, trip, positions.Duplicate)
	f.pool.Cancel(trip.TripID)
	enqueue(t, f.pool, trip, positions.Duplicate)
	if state := f.pool.Snapshot(); state.ActiveWorkers != 1 || state.Pending != 1 {
		t.Fatalf("running state = %+v", state)
	}
	close(release)
	shutdown(t, f.pool)
}

func TestConcurrentEnqueueDeduplicates(t *testing.T) {
	t.Parallel()
	const count = 40
	f := newFixture(t, poolConfig(1, 1), nil)
	trip := candidate()
	barrier := make(chan struct{})
	results := make(chan positions.EnqueueResult, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			result, err := f.pool.Enqueue(context.Background(), trip)
			if err != nil {
				t.Errorf("Enqueue: %v", err)
			}
			results <- result
		}()
	}
	close(barrier)
	wg.Wait()
	close(results)
	queued, duplicates := 0, 0
	for result := range results {
		if result == positions.Queued {
			queued++
		}
		if result == positions.Duplicate {
			duplicates++
		}
	}
	if queued != 1 || duplicates != count-1 {
		t.Fatalf("queued = %d, duplicates = %d", queued, duplicates)
	}
	f.gateway.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).Return(nil)
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
	start(t, f.pool, context.Background())
	shutdown(t, f.pool)
}

func TestPoolRunsFixedNumberOfWorkersAndDrains(t *testing.T) {
	t.Parallel()
	const workers, total = 3, 12
	f := newFixture(t, poolConfig(workers, total), nil)
	began := make(chan struct{}, total)
	release := make(chan struct{})
	var active, maximum atomic.Int64
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(total).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, current) {
				break
			}
		}
		began <- struct{}{}
		return waitRelease(ctx, release)
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).Times(total)
	for range total {
		enqueue(t, f.pool, candidate(), positions.Queued)
	}
	start(t, f.pool, context.Background())
	for range workers {
		select {
		case <-began:
		case <-time.After(3 * time.Second):
			t.Fatal("workers did not start concurrently")
		}
	}
	if state := f.pool.Snapshot(); state.ActiveWorkers != workers || maximum.Load() != workers {
		t.Fatalf("state = %+v, maximum = %d", state, maximum.Load())
	}
	close(release)
	shutdown(t, f.pool)
	if maximum.Load() != workers {
		t.Fatalf("maximum workers = %d, want %d", maximum.Load(), workers)
	}
}

func TestPushErrorDoesNotStopWorker(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 2), nil)
	first, second := candidate(), candidate()
	gomock.InOrder(
		f.gateway.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).Return(errors.New("push unavailable")),
		f.metrics.EXPECT().RequestFinished(gomock.Any(), failed, gomock.Any()),
		f.gateway.EXPECT().RequestPosition(gomock.Any(), second.TripID, second.DriverID).Return(nil),
		f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()),
	)
	enqueue(t, f.pool, first, positions.Queued)
	enqueue(t, f.pool, second, positions.Queued)
	start(t, f.pool, context.Background())
	shutdown(t, f.pool)
}

func TestTaskHasRequestTimeout(t *testing.T) {
	t.Parallel()
	cfg := poolConfig(1, 1)
	cfg.RequestTimeout = 20 * time.Millisecond
	f := newFixture(t, cfg, nil)
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("task context = %v", ctx.Err())
		}
		return ctx.Err()
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), failed, gomock.Any())
	enqueue(t, f.pool, candidate(), positions.Queued)
	start(t, f.pool, context.Background())
	shutdown(t, f.pool)
}

func TestPoolCancellationStopsWorkersAndRejectsNewTasks(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 2), nil)
	started := make(chan struct{})
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), failed, gomock.Any())
	enqueue(t, f.pool, candidate(), positions.Queued)
	enqueue(t, f.pool, candidate(), positions.Queued)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start(t, f.pool, ctx)
	waitFor(t, started)
	cancel()
	waitFor(t, f.pool.Done())
	if _, err := f.pool.Enqueue(context.Background(), candidate()); !errors.Is(err, positions.ErrStopped) {
		t.Fatalf("Enqueue after cancellation: %v", err)
	}
	if state := f.pool.Snapshot(); state != (positions.Snapshot{}) {
		t.Fatalf("state after cancellation = %+v", state)
	}
}

func TestShutdownDeadlineCancelsCurrentRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 1), nil)
	started := make(chan struct{})
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), failed, gomock.Any())
	enqueue(t, f.pool, candidate(), positions.Queued)
	start(t, f.pool, context.Background())
	waitFor(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.pool.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v", err)
	}
	waitFor(t, f.pool.Done())
}

func TestPoolLifecycleAndCancelledEnqueue(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(1, 1), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.pool.Enqueue(ctx, candidate()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Enqueue: %v", err)
	}
	if err := f.pool.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Start: %v", err)
	}
	start(t, f.pool, context.Background())
	if err := f.pool.Start(context.Background()); !errors.Is(err, positions.ErrAlreadyStarted) {
		t.Fatalf("second Start: %v", err)
	}
	shutdown(t, f.pool)
	shutdown(t, f.pool)
	if err := f.pool.Start(context.Background()); !errors.Is(err, positions.ErrStopped) {
		t.Fatalf("Start after shutdown: %v", err)
	}
	other := newFixture(t, poolConfig(1, 1), nil)
	enqueue(t, other.pool, candidate(), positions.Queued)
	shutdown(t, other.pool)
	if _, err := other.pool.Enqueue(context.Background(), candidate()); !errors.Is(err, positions.ErrStopped) {
		t.Fatalf("Enqueue after pre-start shutdown: %v", err)
	}
}

func TestWorkerUsesSpanLinkAndSurvivesProducerCancellation(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("positions-test")
	f := newFixture(t, poolConfig(1, 1), tracer)
	producerCtx, cancel := context.WithCancel(context.Background())
	producerCtx, producerSpan := tracer.Start(producerCtx, "stale scan")
	producer := producerSpan.SpanContext()
	if _, err := f.pool.Enqueue(producerCtx, candidate()); err != nil {
		t.Fatal(err)
	}
	producerSpan.End()
	cancel()
	var taskSpan trace.SpanContext
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		if ctx.Err() != nil {
			t.Errorf("task inherited producer cancellation: %v", ctx.Err())
		}
		taskSpan = trace.SpanContextFromContext(ctx)
		return nil
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
	start(t, f.pool, context.Background())
	shutdown(t, f.pool)
	var worker sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "positions.request" {
			worker = span
		}
	}
	if worker == nil || worker.Parent().IsValid() || len(worker.Links()) != 1 || !worker.Links()[0].SpanContext.Equal(producer) {
		t.Fatalf("invalid asynchronous span: %+v", worker)
	}
	if !worker.SpanContext().Equal(taskSpan) || taskSpan.TraceID() == producer.TraceID() {
		t.Fatal("worker trace or request context does not match")
	}
}
