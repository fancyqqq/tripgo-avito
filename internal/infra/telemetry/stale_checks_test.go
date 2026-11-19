package telemetry_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
)

func TestStaleCheckMetricsCountTripsByEnqueueOutcome(t *testing.T) {
	t.Parallel()
	const (
		queued    = "queued"
		duplicate = "duplicate"
		dropped   = "dropped"
	)
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Errorf("shutdown meter provider: %v", err)
		}
	})
	metrics, err := telemetry.NewStaleCheckMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{queued, queued, duplicate, dropped} {
		metrics.StaleChecked(ctx, result)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	if len(collected.ScopeMetrics) != 1 || len(collected.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("unexpected metrics: %+v", collected)
	}
	metric := collected.ScopeMetrics[0].Metrics[0]
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if metric.Name != "tripgo.stale_checks" || !ok || !sum.IsMonotonic || len(sum.DataPoints) != 3 {
		t.Fatalf("unexpected stale check metric: %+v", metric)
	}
	want := map[string]int64{queued: 2, duplicate: 1, dropped: 1}
	for _, point := range sum.DataPoints {
		result := resultLabel(t, point.Attributes)
		if count, exists := want[result]; !exists || point.Value != count {
			t.Errorf("stale_checks[%s] = %d, want %d", result, point.Value, count)
		}
	}
}
