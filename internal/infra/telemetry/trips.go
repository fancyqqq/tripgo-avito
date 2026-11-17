package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

type TripMetrics struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
	positions metric.Int64Counter
}

func NewTripMetrics(meter metric.Meter) (*TripMetrics, error) {
	created, err := meter.Int64Counter(
		"tripgo.trips.created",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Number of created trips."),
	)
	if err != nil {
		return nil, fmt.Errorf("create trips created counter: %w", err)
	}

	completed, err := meter.Int64Counter(
		"tripgo.trips.completed",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Number of completed trips."),
	)
	if err != nil {
		return nil, fmt.Errorf("create trips completed counter: %w", err)
	}

	positions, err := meter.Int64Counter(
		"tripgo.positions.saved",
		metric.WithUnit("{position}"),
		metric.WithDescription("Number of saved trip positions."),
	)
	if err != nil {
		return nil, fmt.Errorf("create positions saved counter: %w", err)
	}
	return &TripMetrics{created: created, completed: completed, positions: positions}, nil
}

func (m *TripMetrics) TripCreated(ctx context.Context) {
	m.created.Add(ctx, 1)
}

func (m *TripMetrics) TripCompleted(ctx context.Context) {
	m.completed.Add(ctx, 1)
}

func (m *TripMetrics) PositionSaved(ctx context.Context) {
	m.positions.Add(ctx, 1)
}
