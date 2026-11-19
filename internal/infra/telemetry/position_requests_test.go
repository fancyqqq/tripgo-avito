package telemetry_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
)

func TestPositionRequestMetricsSeparateDropsFromCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(ctx); err != nil {
			t.Errorf("shutdown meter provider: %v", err)
		}
	})
	metrics, err := telemetry.NewPositionRequestMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	metrics.RequestFinished(ctx, "success", 250*time.Millisecond)
	metrics.RequestFinished(ctx, "error", 500*time.Millisecond)
	metrics.RequestDropped(ctx)
	metrics.RequestDropped(ctx)
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	if len(collected.ScopeMetrics) != 1 || len(collected.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("unexpected metrics: %+v", collected)
	}
	for _, metric := range collected.ScopeMetrics[0].Metrics {
		switch metric.Name {
		case "tripgo.position_requests":
			assertRequestCounts(t, metric)
		case "tripgo.position_requests.duration":
			assertRequestDurations(t, metric)
		default:
			t.Fatalf("unexpected metric %s", metric.Name)
		}
	}
}

func assertRequestCounts(t *testing.T, metric metricdata.Metrics) {
	t.Helper()
	sum, ok := metric.Data.(metricdata.Sum[int64])
	if !ok || !sum.IsMonotonic || len(sum.DataPoints) != 3 {
		t.Fatalf("unexpected request counts: %+v", metric)
	}
	want := map[string]int64{"success": 1, "error": 1, "dropped": 2}
	for _, point := range sum.DataPoints {
		result := resultLabel(t, point.Attributes)
		if count, exists := want[result]; !exists || point.Value != count {
			t.Errorf("requests[%s] = %d, want %d", result, point.Value, count)
		}
	}
}

func assertRequestDurations(t *testing.T, metric metricdata.Metrics) {
	t.Helper()
	histogram, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok || metric.Unit != "s" || len(histogram.DataPoints) != 2 {
		t.Fatalf("unexpected durations: %+v", metric)
	}
	want := map[string]float64{"success": 0.25, "error": 0.5}
	for _, point := range histogram.DataPoints {
		result := resultLabel(t, point.Attributes)
		if seconds, exists := want[result]; !exists || point.Sum != seconds || point.Count != 1 {
			t.Errorf("duration[%s] = %f (%d calls), want %f", result, point.Sum, point.Count, seconds)
		}
	}
}

func resultLabel(t *testing.T, attrs attribute.Set) string {
	t.Helper()
	value, ok := attrs.Value(attribute.Key("result"))
	if !ok || attrs.Len() != 1 {
		t.Fatalf("expected only result label: %v", attrs)
	}
	return value.AsString()
}
