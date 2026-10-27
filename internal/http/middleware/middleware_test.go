package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/http/middleware"
)

const routeTemplate = "/api/v1/trips/{tripId}"

func newRouter() chi.Router {
	r := chi.NewRouter()
	r.Get(routeTemplate, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	r.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	return r
}

func tripPath() string {
	return "/api/v1/trips/" + uuid.NewString()
}

func TestChainOrder(t *testing.T) {
	t.Parallel()

	var calls []string
	mark := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := middleware.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls = append(calls, "handler")
	}), mark("first"), mark("second"), mark("third"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"first", "second", "third", "handler"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestTracingUsesRouteTemplate(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	router := newRouter()
	h := middleware.Chain(router, middleware.Tracing(router, otelhttp.WithTracerProvider(tp)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tripPath(), nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if got, want := span.Name(), "GET "+routeTemplate; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}

	var route string
	for _, a := range span.Attributes() {
		if a.Key == "http.route" {
			route = a.Value.AsString()
		}
	}
	if route != routeTemplate {
		t.Fatalf("http.route = %q, want %q", route, routeTemplate)
	}
}

func TestTracingUnknownRouteHasNoTemplate(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	router := newRouter()
	h := middleware.Chain(router, middleware.Tracing(router, otelhttp.WithTracerProvider(tp)))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/unknown/123", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != http.MethodGet {
		t.Fatalf("span name = %q, want %q", got, http.MethodGet)
	}
}

func TestMetricsRecordRouteTemplate(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := middleware.NewMetrics(mp.Meter("test"))
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}

	router := newRouter()
	tp := sdktrace.NewTracerProvider()
	h := middleware.Chain(router, middleware.Tracing(router, otelhttp.WithTracerProvider(tp)), metrics.Middleware)

	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tripPath(), nil))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	duration, ok := findMetric(rm, "http.server.request.duration").Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatal("http.server.request.duration is not a float64 histogram")
	}
	if len(duration.DataPoints) != 1 {
		t.Fatalf("duration points = %d, want 1 (one route template)", len(duration.DataPoints))
	}
	point := duration.DataPoints[0]
	if point.Count != 3 {
		t.Fatalf("duration count = %d, want 3", point.Count)
	}
	assertAttr(t, point.Attributes, "http.route", attribute.StringValue(routeTemplate))
	assertAttr(t, point.Attributes, "http.request.method", attribute.StringValue(http.MethodGet))
	assertAttr(t, point.Attributes, "http.response.status_code", attribute.IntValue(http.StatusCreated))

	active, ok := findMetric(rm, "http.server.active_requests").Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatal("http.server.active_requests is not an int64 sum")
	}
	for _, p := range active.DataPoints {
		if p.Value != 0 {
			t.Fatalf("active requests = %d after all requests finished, want 0", p.Value)
		}
	}
}

func TestLoggingWritesRequestFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	router := newRouter()
	h := middleware.Chain(router,
		middleware.Tracing(router, otelhttp.WithTracerProvider(sdktrace.NewTracerProvider())),
		middleware.Logging(logger),
	)

	path := tripPath()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry %q: %v", buf.String(), err)
	}

	want := map[string]any{
		"level":  "INFO",
		"msg":    "http request",
		"method": http.MethodGet,
		"route":  routeTemplate,
		"path":   path,
		"status": float64(http.StatusCreated),
	}
	for k, v := range want {
		if entry[k] != v {
			t.Fatalf("log %s = %v, want %v (entry %v)", k, entry[k], v, entry)
		}
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("log entry has no duration_ms: %v", entry)
	}
}

func TestRecoverReturnsProblem(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	router := newRouter()
	h := middleware.Chain(router, middleware.Recover(logger))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var p generated.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", p.Code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("panic: boom")) {
		t.Fatalf("panic is not logged: %s", buf.String())
	}
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	t.Parallel()

	h := middleware.Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Fatalf("recovered = %v, want %v", p, http.ErrAbortHandler)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func findMetric(rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	return metricdata.Metrics{}
}

func assertAttr(t *testing.T, set attribute.Set, key string, want attribute.Value) {
	t.Helper()

	got, ok := set.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %s is missing in %v", key, set.ToSlice())
	}
	if got != want {
		t.Fatalf("attribute %s = %v, want %v", key, got.String(), want.String())
	}
}
