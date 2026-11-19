package telemetry_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
)

func TestPositionPoolGaugesReadOneSnapshotPerCollection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })
	var queued, active, reads int
	if err := telemetry.RegisterPositionPoolMetrics(provider.Meter("test"), func() (int, int) {
		reads++
		return queued, active
	}); err != nil {
		t.Fatal(err)
	}
	for i, state := range [][2]int{{0, 0}, {10, 4}, {0, 4}, {0, 0}} {
		queued, active = state[0], state[1]
		var collected metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &collected); err != nil {
			t.Fatal(err)
		}
		if reads != i+1 || len(collected.ScopeMetrics) != 1 || len(collected.ScopeMetrics[0].Metrics) != 2 {
			t.Fatalf("snapshot reads = %d, metrics = %+v", reads, collected)
		}
		for _, metric := range collected.ScopeMetrics[0].Metrics {
			want, unit := int64(queued), "{task}"
			switch metric.Name {
			case "tripgo.position_requests.queue.size":
			case "tripgo.position_requests.workers.active":
				want, unit = int64(active), "{worker}"
			default:
				t.Fatalf("unexpected metric %s", metric.Name)
			}
			gauge, ok := metric.Data.(metricdata.Gauge[int64])
			if !ok || metric.Unit != unit || len(gauge.DataPoints) != 1 {
				t.Fatalf("unexpected gauge: %+v", metric)
			}
			point := gauge.DataPoints[0]
			if point.Value != want || point.Attributes.Len() != 0 {
				t.Errorf("%s = %d, attributes = %v, want %d without labels", metric.Name, point.Value, point.Attributes, want)
			}
		}
	}
}

func TestPositionPoolMetricsRejectNilStateReader(t *testing.T) {
	t.Parallel()
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	if err := telemetry.RegisterPositionPoolMetrics(provider.Meter("test"), nil); err == nil {
		t.Fatal("nil state reader accepted")
	}
}
