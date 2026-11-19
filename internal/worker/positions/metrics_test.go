package positions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

func collectPoolGauges(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	values := make(map[string]int64)
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			gauge, ok := metric.Data.(metricdata.Gauge[int64])
			if !ok || len(gauge.DataPoints) != 1 {
				t.Fatalf("unexpected pool gauge: %+v", metric)
			}
			values[metric.Name] = gauge.DataPoints[0].Value
		}
	}
	return values
}

func assertPoolGauges(t *testing.T, reader *sdkmetric.ManualReader, queued, active int64) {
	t.Helper()
	values := collectPoolGauges(t, reader)
	if len(values) != 2 || values["tripgo.position_requests.queue.size"] != queued || values["tripgo.position_requests.workers.active"] != active {
		t.Fatalf("pool metrics = %v, want queue %d, active %d", values, queued, active)
	}
}

func TestPoolGaugesTrackQueueWorkersAndShutdown(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	f := newFixture(t, poolConfig(2, 3), nil)
	if err := telemetry.RegisterPositionPoolMetrics(provider.Meter("test"), func() (int, int) {
		state := f.pool.Snapshot()
		return state.QueueSize, state.ActiveWorkers
	}); err != nil {
		t.Fatal(err)
	}
	assertPoolGauges(t, reader, 0, 0)
	started, release := make(chan struct{}, 3), make(chan struct{})
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Times(3).DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
		started <- struct{}{}
		return waitRelease(ctx, release)
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).Times(3)
	for range 3 {
		enqueue(t, f.pool, candidate(), positions.Queued)
	}
	assertPoolGauges(t, reader, 3, 0)
	start(t, f.pool, context.Background())
	for range 2 {
		waitFor(t, started)
	}
	assertPoolGauges(t, reader, 1, 2)
	close(release)
	shutdown(t, f.pool)
	assertPoolGauges(t, reader, 0, 0)
}

func TestPoolGaugeIncludesCancelledTasksUntilChannelIsRead(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	f := newFixture(t, poolConfig(1, 1), nil)
	if err := telemetry.RegisterPositionPoolMetrics(provider.Meter("test"), func() (int, int) {
		state := f.pool.Snapshot()
		return state.QueueSize, state.ActiveWorkers
	}); err != nil {
		t.Fatal(err)
	}
	trip := candidate()
	enqueue(t, f.pool, trip, positions.Queued)
	f.pool.Cancel(trip.TripID)
	assertPoolGauges(t, reader, 1, 0)
	start(t, f.pool, context.Background())
	shutdown(t, f.pool)
	assertPoolGauges(t, reader, 0, 0)
}
