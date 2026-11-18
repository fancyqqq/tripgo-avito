package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/fancyqqq/tripgo-avito/internal/gateway/push"
	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

type tripSource func(context.Context, time.Duration) ([]model.StaleTrip, error)

func (f tripSource) ListStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error) {
	return f(ctx, staleAfter)
}

type scanObserver func(context.Context, string)

func (f scanObserver) StaleChecked(ctx context.Context, result string) {
	f(ctx, result)
}

type manualTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
}

func (t *manualTicker) C() <-chan time.Time { return t.ticks }
func (t *manualTicker) Stop()               { close(t.stopped) }

type observedHTTPServer struct {
	*http.Server
	stopping chan struct{}
}

func (s observedHTTPServer) Shutdown(ctx context.Context) error {
	close(s.stopping)
	return s.Server.Shutdown(ctx)
}

type pipelineFixture struct {
	scanner          *positions.Scanner
	workers          *positions.Pool
	gateway          *push.Gateway
	ticker           *manualTicker
	batchDone        chan struct{}
	requestStarted   chan struct{}
	requestCancelled chan struct{}
	release          chan struct{}
	releaseOnce      sync.Once
	mu               sync.Mutex
	requests         map[uuid.UUID]int
	active           atomic.Int64
	maximum          int64
}

func newPipeline(t *testing.T, workers, total int) *pipelineFixture {
	t.Helper()
	f := &pipelineFixture{
		ticker:    &manualTicker{ticks: make(chan time.Time, 1), stopped: make(chan struct{})},
		batchDone: make(chan struct{}), requestStarted: make(chan struct{}, total), requestCancelled: make(chan struct{}, total),
		release: make(chan struct{}), requests: make(map[uuid.UUID]int),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data struct {
				TripID uuid.UUID `json:"trip_id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode push: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		current := f.active.Add(1)
		defer f.active.Add(-1)
		f.mu.Lock()
		f.requests[body.Data.TripID]++
		if current > f.maximum {
			f.maximum = current
		}
		f.mu.Unlock()
		f.requestStarted <- struct{}{}
		select {
		case <-f.release:
			w.WriteHeader(http.StatusAccepted)
		case <-r.Context().Done():
			f.requestCancelled <- struct{}{}
		}
	}))
	t.Cleanup(server.Close)
	var err error
	f.gateway, err = push.New(server.URL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.gateway.Close)
	metrics, err := telemetry.NewPositionRequestMetrics(metricnoop.NewMeterProvider().Meter("shutdown-test"))
	if err != nil {
		t.Fatal(err)
	}
	tracer := tracenoop.NewTracerProvider().Tracer("shutdown-test")
	logger := slog.New(slog.DiscardHandler)
	f.workers, err = positions.NewPool(positions.PoolConfig{Workers: workers, QueueSize: total, RequestTimeout: 10 * time.Second}, f.gateway, metrics, tracer, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.workers.Close)
	trips := make([]model.StaleTrip, total)
	for i := range trips {
		trips[i] = model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	}
	var queued int
	source := tripSource(func(context.Context, time.Duration) ([]model.StaleTrip, error) { return trips, nil })
	observer := scanObserver(func(_ context.Context, result string) {
		if result != string(positions.Queued) {
			t.Errorf("unexpected enqueue result: %s", result)
		}
		queued++
		if queued == total {
			close(f.batchDone)
		}
	})
	f.scanner, err = positions.NewScanner(positions.ScannerConfig{CheckInterval: time.Hour, StaleAfter: time.Minute}, source, f.workers, observer, tracer, logger, func(time.Duration) positions.Ticker { return f.ticker })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.scanner.Close)
	t.Cleanup(func() { f.releaseOnce.Do(func() { close(f.release) }) })
	if err := f.workers.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.scanner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.ticker.ticks <- time.Now()
	await(t, f.batchDone)
	for range workers {
		await(t, f.requestStarted)
	}
	return f
}

func await(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}
}

func (f *pipelineFixture) closeResources(t *testing.T) {
	t.Helper()
	for name, done := range map[string]<-chan struct{}{"scanner": f.scanner.Done(), "workers": f.workers.Done()} {
		select {
		case <-done:
		default:
			t.Errorf("resources closed before %s exited", name)
		}
	}
	f.gateway.Close()
}

func TestShutdownDrainsLoadedPipelineAfterCurrentHTTPRequest(t *testing.T) {
	t.Parallel()
	const workers, total = 3, 12
	f := newPipeline(t, workers, total)
	httpEntered, httpRelease := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(httpEntered)
		select {
		case <-httpRelease:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	response := make(chan error, 1)
	go func() {
		res, err := server.Client().Get(server.URL)
		if err == nil {
			err = res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				err = errors.New("current HTTP request did not finish successfully")
			}
		}
		response <- err
	}()
	await(t, httpEntered)
	stopping := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, joined := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(joined)
		result <- shutdown(ctx, observedHTTPServer{Server: server.Config, stopping: stopping}, f.scanner, f.workers, func() { f.closeResources(t) })
	}()
	t.Cleanup(func() { cancel(); await(t, joined) })
	await(t, stopping)
	select {
	case <-f.ticker.stopped:
		t.Fatal("scanner stopped while HTTP request was still active")
	default:
	}
	close(httpRelease)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	await(t, f.scanner.Done())
	select {
	case <-f.workers.Done():
		t.Fatal("workers stopped before queued requests finished")
	default:
	}
	f.releaseOnce.Do(func() { close(f.release) })
	if err := <-result; err != nil {
		t.Fatalf("graceful shutdown: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != total || f.maximum != workers {
		t.Fatalf("requests = %d/%d, maximum active = %d", len(f.requests), total, f.maximum)
	}
	for id, count := range f.requests {
		if count != 1 {
			t.Errorf("trip %s sent %d times", id, count)
		}
	}
	if state := f.workers.Snapshot(); state != (positions.Snapshot{}) {
		t.Fatalf("state after shutdown: %+v", state)
	}
}

func TestShutdownExpiredBudgetCancelsHTTPAndPushRequests(t *testing.T) {
	t.Parallel()
	const workers, total = 2, 8
	f := newPipeline(t, workers, total)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	t.Cleanup(server.Close)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := server.Client().Get(server.URL)
		if err == nil {
			_ = res.Body.Close()
		}
	}()
	await(t, entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	err := shutdown(ctx, server.Config, f.scanner, f.workers, func() { f.closeResources(t) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
	await(t, cancelled)
	await(t, clientDone)
	for range workers {
		await(t, f.requestCancelled)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != workers {
		t.Fatalf("expired shutdown sent queued requests: %d", len(f.requests))
	}
	if state := f.workers.Snapshot(); state != (positions.Snapshot{}) {
		t.Fatalf("state after forced shutdown: %+v", state)
	}
}
