package middleware

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

type Metrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create request duration histogram: %w", err)
	}

	active, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of active HTTP server requests."),
	)
	if err != nil {
		return nil, fmt.Errorf("create active requests counter: %w", err)
	}

	return &Metrics{duration: duration, active: active}, nil
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()

		attrs := []attribute.KeyValue{attribute.String("http.request.method", r.Method)}
		if r.Pattern != "" {
			attrs = append(attrs, attribute.String("http.route", r.Pattern))
		}

		activeAttrs := metric.WithAttributes(attrs...)
		m.active.Add(ctx, 1, activeAttrs)
		defer m.active.Add(ctx, -1, activeAttrs)

		rec := newStatusRecorder(w)
		next.ServeHTTP(rec, r)

		attrs = append(attrs, attribute.Int("http.response.status_code", rec.status))
		m.duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	})
}
