package telemetry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type traceExporter struct {
	export func(context.Context, []sdktrace.ReadOnlySpan) error
	closed chan struct{}
}

func (e traceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return e.export(ctx, spans)
}

func (e traceExporter) Shutdown(context.Context) error {
	close(e.closed)
	return nil
}

func waitTraceEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for trace processor shutdown")
	}
}

func TestTracingShutdownExportsAndJoinsBatchProcessor(t *testing.T) {
	t.Parallel()
	var exported atomic.Int64
	closed := make(chan struct{})
	exporter := traceExporter{closed: closed, export: func(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
		if ctx.Err() != nil {
			t.Errorf("graceful export was cancelled: %v", ctx.Err())
		}
		exported.Add(int64(len(spans)))
		return nil
	}}
	provider, shutdown := newTraceProvider(exporter, resource.Empty())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = shutdown(ctx) })
	_, span := provider.Tracer("test").Start(context.Background(), "last task")
	span.End()
	if err := shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if exported.Load() != 1 {
		t.Fatalf("exported spans = %d", exported.Load())
	}
	waitTraceEvent(t, closed)
}

func TestTracingExpiredShutdownCancelsActiveExportAndJoinsProcessor(t *testing.T) {
	t.Parallel()
	entered, exited, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	exporter := traceExporter{closed: closed, export: func(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
		close(entered)
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	}}
	provider, shutdown := newTraceProvider(exporter, resource.Empty())
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	t.Cleanup(func() { cancelCleanup(); _ = shutdown(cleanupCtx) })
	_, span := provider.Tracer("test").Start(context.Background(), "last task")
	span.End()
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		_ = provider.ForceFlush(cleanupCtx)
	}()
	waitTraceEvent(t, entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	if err := shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired tracing shutdown: %v", err)
	}
	waitTraceEvent(t, exited)
	waitTraceEvent(t, closed)
	waitTraceEvent(t, flushed)
}

func TestTracingAlreadyExpiredShutdownStillStopsProcessor(t *testing.T) {
	t.Parallel()
	closed := make(chan struct{})
	exporter := traceExporter{closed: closed, export: func(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	provider, shutdown := newTraceProvider(exporter, resource.Empty())
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	t.Cleanup(func() { _ = shutdown(ctx) })
	_, span := provider.Tracer("test").Start(context.Background(), "queued span")
	span.End()
	if err := shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired tracing shutdown: %v", err)
	}
	waitTraceEvent(t, closed)
}
