package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type PositionRequestMetrics struct {
	requests metric.Int64Counter
	duration metric.Float64Histogram
}

func NewPositionRequestMetrics(meter metric.Meter) (*PositionRequestMetrics, error) {
	requests, err := meter.Int64Counter("tripgo.position_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create position requests counter: %w", err)
	}
	duration, err := meter.Float64Histogram("tripgo.position_requests.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create position request duration: %w", err)
	}
	return &PositionRequestMetrics{requests: requests, duration: duration}, nil
}

func (m *PositionRequestMetrics) RequestDropped(ctx context.Context) {
	m.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "dropped")))
}

func (m *PositionRequestMetrics) RequestFinished(ctx context.Context, result string, duration time.Duration) {
	options := metric.WithAttributes(attribute.String("result", result))
	m.requests.Add(ctx, 1, options)
	m.duration.Record(ctx, duration.Seconds(), options)
}
