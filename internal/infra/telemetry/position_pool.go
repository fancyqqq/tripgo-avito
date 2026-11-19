package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

func RegisterPositionPoolMetrics(meter metric.Meter, readState func() (queueSize, activeWorkers int)) error {
	if readState == nil {
		return fmt.Errorf("position pool state reader must not be nil")
	}
	queueSize, err := meter.Int64ObservableGauge("tripgo.position_requests.queue.size", metric.WithUnit("{task}"))
	if err != nil {
		return fmt.Errorf("create position queue size gauge: %w", err)
	}
	activeWorkers, err := meter.Int64ObservableGauge("tripgo.position_requests.workers.active", metric.WithUnit("{worker}"))
	if err != nil {
		return fmt.Errorf("create active position workers gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		queued, active := readState()
		observer.ObserveInt64(queueSize, int64(queued))
		observer.ObserveInt64(activeWorkers, int64(active))
		return nil
	}, queueSize, activeWorkers)
	if err != nil {
		return fmt.Errorf("register position pool metric callback: %w", err)
	}
	return nil
}
