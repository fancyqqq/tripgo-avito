package positions

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	resultSuccess   = "success"
	resultError     = "error"
	resultCancelled = "cancelled"
)

func (p *Pool) work(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case queued, ok := <-p.queue:
			if !ok || ctx.Err() != nil {
				return
			}
			p.process(ctx, queued)
		}
	}
}

func (p *Pool) process(ctx context.Context, queued *task) {
	options := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithAttributes(attribute.String("trip_id", queued.trip.TripID.String()))}
	if queued.origin.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: queued.origin}))
	}
	ctx, span := p.tracer.Start(ctx, "positions.request", options...)
	defer span.End()
	if !p.claim(queued) {
		span.SetAttributes(attribute.String("result", resultCancelled))
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, p.config.RequestTimeout)
	defer cancel()
	started := time.Now()
	err := p.gateway.RequestPosition(requestCtx, queued.trip.TripID, queued.trip.DriverID)
	duration := time.Since(started)
	p.complete(queued)
	result := resultSuccess
	if err != nil {
		result = resultError
		span.RecordError(err)
		span.SetStatus(codes.Error, "position request failed")
		p.logger.WarnContext(ctx, "position request failed", "trip_id", queued.trip.TripID, "error", err)
	}
	span.SetAttributes(attribute.String("result", result))
	p.metrics.RequestFinished(ctx, result, duration)
}

func (p *Pool) claim(queued *task) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[queued.trip.TripID] != queued || queued.running {
		return false
	}
	queued.running = true
	p.active++
	return true
}

func (p *Pool) complete(queued *task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[queued.trip.TripID] == queued {
		delete(p.pending, queued.trip.TripID)
	}
	p.active--
}
