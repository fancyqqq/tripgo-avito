package telemetry

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type cancellableSpanExporter struct {
	delegate sdktrace.SpanExporter
	ctx      context.Context
	done     chan struct{}
}

func (e *cancellableSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	defer cancel()
	return e.delegate.ExportSpans(ctx, spans)
}

func (e *cancellableSpanExporter) Shutdown(ctx context.Context) error {
	defer close(e.done)
	return e.delegate.Shutdown(ctx)
}

func newTraceProvider(exporter sdktrace.SpanExporter, res *resource.Resource) (*sdktrace.TracerProvider, func(context.Context) error) {
	exportCtx, cancelExports := context.WithCancel(context.Background())
	wrapped := &cancellableSpanExporter{delegate: exporter, ctx: exportCtx, done: make(chan struct{})}
	processor := sdktrace.NewBatchSpanProcessor(wrapped)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor), sdktrace.WithResource(res))
	return provider, func(ctx context.Context) error {
		stop := context.AfterFunc(ctx, cancelExports)
		defer stop()
		processorErr := processor.Shutdown(ctx)
		cancelExports()
		<-wrapped.done
		return errors.Join(processorErr, provider.Shutdown(ctx))
	}
}
