package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type StaleCheckMetrics struct {
	checks metric.Int64Counter
}

func NewStaleCheckMetrics(meter metric.Meter) (*StaleCheckMetrics, error) {
	checks, err := meter.Int64Counter("tripgo.stale_checks", metric.WithUnit("{trip}"))
	if err != nil {
		return nil, fmt.Errorf("create stale checks counter: %w", err)
	}
	return &StaleCheckMetrics{checks: checks}, nil
}

func (m *StaleCheckMetrics) StaleChecked(ctx context.Context, result string) {
	m.checks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
