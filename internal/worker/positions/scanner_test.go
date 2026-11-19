package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

type scannerFixture struct {
	scanner *positions.Scanner
	trips   *MockstaleTrips
	queue   *MockpositionQueue
	metrics *MockscanMetrics
	ticker  *MockTicker
	ticks   chan time.Time
}

type scannerQueue interface {
	Enqueue(ctx context.Context, trip model.StaleTrip) (positions.EnqueueResult, error)
}

func scannerConfig() positions.ScannerConfig {
	return positions.ScannerConfig{CheckInterval: 37 * time.Second, StaleAfter: 83 * time.Second}
}

func newScannerFixture(t *testing.T, tracer trace.Tracer) scannerFixture {
	t.Helper()
	return newScannerFixtureWithQueue(t, tracer, nil)
}

func newScannerFixtureWithQueue(t *testing.T, tracer trace.Tracer, queue scannerQueue) scannerFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := scannerFixture{
		trips: NewMockstaleTrips(ctrl), queue: NewMockpositionQueue(ctrl), metrics: NewMockscanMetrics(ctrl),
		ticker: NewMockTicker(ctrl), ticks: make(chan time.Time, 1),
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("scanner-test")
	}
	if queue == nil {
		queue = f.queue
	}
	var err error
	f.scanner, err = positions.NewScanner(scannerConfig(), f.trips, queue, f.metrics, tracer, slog.New(slog.DiscardHandler), func(interval time.Duration) positions.Ticker {
		if interval != scannerConfig().CheckInterval {
			t.Errorf("ticker interval = %v", interval)
		}
		return f.ticker
	})
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	t.Cleanup(f.scanner.Close)
	return f
}

func startScanner(t *testing.T, f scannerFixture, ctx context.Context) {
	t.Helper()
	f.ticker.EXPECT().C().Return((<-chan time.Time)(f.ticks))
	f.ticker.EXPECT().Stop()
	if err := f.scanner.Start(ctx); err != nil {
		t.Fatalf("Start scanner: %v", err)
	}
}

func stopScanner(t *testing.T, scanner *positions.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := scanner.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown scanner: %v", err)
	}
}

func tick(t *testing.T, ticks chan<- time.Time) {
	t.Helper()
	select {
	case ticks <- time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC):
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not consume tick")
	}
}

func TestScannerEnqueuesSQLResultsInOrderAndCountsOutcomes(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	trips := []model.StaleTrip{candidate(), candidate(), candidate()}
	finished := make(chan struct{})
	gomock.InOrder(
		f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return(trips, nil),
		f.queue.EXPECT().Enqueue(gomock.Any(), trips[0]).Return(positions.Queued, nil),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued)),
		f.queue.EXPECT().Enqueue(gomock.Any(), trips[1]).Return(positions.Duplicate, nil),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Duplicate)),
		f.queue.EXPECT().Enqueue(gomock.Any(), trips[2]).Return(positions.Dropped, nil),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Dropped)).Do(func(context.Context, string) { close(finished) }),
	)
	startScanner(t, f, context.Background())
	tick(t, f.ticks)
	waitFor(t, finished)
	stopScanner(t, f.scanner)
}

func TestScannerProcessesTicksSequentially(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	entered, nextScan, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls, active, maximum atomic.Int64
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Times(2).DoAndReturn(func(ctx context.Context, _ time.Duration) ([]model.StaleTrip, error) {
		current := active.Add(1)
		defer active.Add(-1)
		if current > maximum.Load() {
			maximum.Store(current)
		}
		if calls.Add(1) == 1 {
			close(entered)
			return nil, waitRelease(ctx, release)
		}
		close(nextScan)
		return nil, nil
	})
	startScanner(t, f, context.Background())
	tick(t, f.ticks)
	waitFor(t, entered)
	tick(t, f.ticks)
	select {
	case <-nextScan:
		t.Fatal("second scan started before first scan completed")
	default:
	}
	close(release)
	waitFor(t, nextScan)
	stopScanner(t, f.scanner)
	if maximum.Load() != 1 {
		t.Fatalf("simultaneous scans = %d", maximum.Load())
	}
}

func TestScannerContinuesAfterDatabaseError(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	f := newScannerFixture(t, provider.Tracer("scanner-test"))
	trip := candidate()
	failedScan, finished := make(chan struct{}), make(chan struct{})
	gomock.InOrder(
		f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).DoAndReturn(func(context.Context, time.Duration) ([]model.StaleTrip, error) {
			close(failedScan)
			return nil, errors.New("database unavailable")
		}),
		f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return([]model.StaleTrip{trip}, nil),
		f.queue.EXPECT().Enqueue(gomock.Any(), trip).Return(positions.Queued, nil),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued)).Do(func(context.Context, string) { close(finished) }),
	)
	startScanner(t, f, context.Background())
	tick(t, f.ticks)
	waitFor(t, failedScan)
	tick(t, f.ticks)
	waitFor(t, finished)
	stopScanner(t, f.scanner)
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Status().Code != codes.Error || spans[1].Status().Code == codes.Error {
		t.Fatalf("unexpected scan spans: %+v", spans)
	}
}

func TestScannerCancellationInterruptsDatabaseQuery(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	entered := make(chan struct{})
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).DoAndReturn(func(ctx context.Context, _ time.Duration) ([]model.StaleTrip, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startScanner(t, f, ctx)
	tick(t, f.ticks)
	waitFor(t, entered)
	cancel()
	waitFor(t, f.scanner.Done())
	stopScanner(t, f.scanner)
}

func TestScannerCancellationStopsBatchAfterAcceptedTask(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	trips := []model.StaleTrip{candidate(), candidate()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return(trips, nil)
	f.queue.EXPECT().Enqueue(gomock.Any(), trips[0]).DoAndReturn(func(context.Context, model.StaleTrip) (positions.EnqueueResult, error) {
		cancel()
		return positions.Queued, nil
	})
	f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued))
	startScanner(t, f, ctx)
	tick(t, f.ticks)
	waitFor(t, f.scanner.Done())
}

func TestScannerDoesNotCountFailedEnqueueAsOutcome(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	trip := candidate()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return([]model.StaleTrip{trip}, nil)
	f.queue.EXPECT().Enqueue(gomock.Any(), trip).DoAndReturn(func(context.Context, model.StaleTrip) (positions.EnqueueResult, error) {
		cancel()
		return "", positions.ErrStopped
	})
	startScanner(t, f, ctx)
	tick(t, f.ticks)
	waitFor(t, f.scanner.Done())
}

func TestScannerLifecycle(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.scanner.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Start: %v", err)
	}
	startScanner(t, f, context.Background())
	if err := f.scanner.Start(context.Background()); !errors.Is(err, positions.ErrAlreadyStarted) {
		t.Fatalf("second Start: %v", err)
	}
	stopScanner(t, f.scanner)
	stopScanner(t, f.scanner)
	if err := f.scanner.Start(context.Background()); !errors.Is(err, positions.ErrStopped) {
		t.Fatalf("Start after shutdown: %v", err)
	}
	other := newScannerFixture(t, nil)
	stopScanner(t, other.scanner)
	if err := other.scanner.Start(context.Background()); !errors.Is(err, positions.ErrStopped) {
		t.Fatalf("Start after pre-start shutdown: %v", err)
	}
}

func TestScannerShutdownRespectsDeadline(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).DoAndReturn(func(context.Context, time.Duration) ([]model.StaleTrip, error) {
		close(entered)
		<-release
		return nil, nil
	})
	startScanner(t, f, context.Background())
	tick(t, f.ticks)
	waitFor(t, entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	if err := f.scanner.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown: %v", err)
	}
	once.Do(func() { close(release) })
	waitFor(t, f.scanner.Done())
}

func TestScannerRejectsInvalidConfigAndDependencies(t *testing.T) {
	t.Parallel()
	f := newScannerFixture(t, nil)
	for _, cfg := range []positions.ScannerConfig{
		{CheckInterval: 0, StaleAfter: time.Second},
		{CheckInterval: time.Second, StaleAfter: 0},
		{CheckInterval: -time.Second, StaleAfter: time.Second},
	} {
		scanner, err := positions.NewScanner(cfg, f.trips, f.queue, f.metrics, noop.NewTracerProvider().Tracer("test"), slog.New(slog.DiscardHandler), positions.NewTicker)
		if err == nil {
			scanner.Close()
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	if scanner, err := positions.NewScanner(scannerConfig(), nil, nil, nil, nil, nil, nil); err == nil {
		scanner.Close()
		t.Fatal("nil dependencies accepted")
	}
}
