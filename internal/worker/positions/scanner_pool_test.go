package positions_test

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

func TestScannerRepeatedTicksDeduplicateAndDropWithoutBlocking(t *testing.T) {
	t.Parallel()
	pool := newFixture(t, poolConfig(1, 1), nil)
	f := newScannerFixtureWithQueue(t, nil, pool.pool)
	first, second := candidate(), candidate()
	finished := make(chan struct{}, 2)
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return([]model.StaleTrip{first, second}, nil).Times(2)
	gomock.InOrder(
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued)),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Dropped)).Do(func(context.Context, string) { finished <- struct{}{} }),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Duplicate)),
		f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Dropped)).Do(func(context.Context, string) { finished <- struct{}{} }),
	)
	pool.metrics.EXPECT().RequestDropped(gomock.Any()).Times(2)
	pool.gateway.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).Return(nil)
	pool.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
	startScanner(t, f, context.Background())
	for range 2 {
		tick(t, f.ticks)
		waitFor(t, finished)
	}
	stopScanner(t, f.scanner)
	if state := pool.pool.Snapshot(); state.QueueSize != 1 || state.Pending != 1 {
		t.Fatalf("queue after repeated ticks: %+v", state)
	}
	start(t, pool.pool, context.Background())
	shutdown(t, pool.pool)
}

func TestScannerCanRequeueCompletedRequestAndLinksWorkerSpans(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("scanner-pool-test")
	pool := newFixture(t, poolConfig(1, 1), tracer)
	f := newScannerFixtureWithQueue(t, tracer, pool.pool)
	trip := candidate()
	scanDone, requestDone := make(chan struct{}, 2), make(chan struct{}, 2)
	f.trips.EXPECT().ListStale(gomock.Any(), scannerConfig().StaleAfter).Return([]model.StaleTrip{trip}, nil).Times(2)
	f.metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued)).Times(2).Do(func(context.Context, string) { scanDone <- struct{}{} })
	pool.gateway.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).Return(nil).Times(2)
	pool.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).Times(2).Do(func(context.Context, string, time.Duration) { requestDone <- struct{}{} })
	start(t, pool.pool, context.Background())
	startScanner(t, f, context.Background())
	for range 2 {
		tick(t, f.ticks)
		waitFor(t, scanDone)
		waitFor(t, requestDone)
	}
	stopScanner(t, f.scanner)
	shutdown(t, pool.pool)
	assertScanLinks(t, recorder.Ended())
}

func assertScanLinks(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	scans := make(map[string]bool)
	requests := make([]sdktrace.ReadOnlySpan, 0, 2)
	for _, span := range spans {
		switch span.Name() {
		case "positions.scan":
			scans[span.SpanContext().TraceID().String()] = true
		case "positions.request":
			requests = append(requests, span)
		}
	}
	if len(scans) != 2 || len(requests) != 2 {
		t.Fatalf("scan traces = %d, requests = %d", len(scans), len(requests))
	}
	for _, request := range requests {
		if request.Parent().IsValid() || len(request.Links()) != 1 {
			t.Fatalf("invalid worker span: %+v", request)
		}
		origin := request.Links()[0].SpanContext.TraceID().String()
		if !scans[origin] {
			t.Fatalf("worker linked to unknown scan %s", origin)
		}
		delete(scans, origin)
	}
}
