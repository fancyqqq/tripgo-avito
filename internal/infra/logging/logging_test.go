package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/fancyqqq/tripgo-avito/internal/infra/logging"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry %q: %v", buf.String(), err)
	}
	return entry
}

func TestNewAddsServiceAndTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := logging.New(&buf, "info", "trip-service")
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()

	logger.With("component", "test").InfoContext(ctx, "hello")

	entry := decode(t, &buf)
	if entry["service"] != "trip-service" {
		t.Fatalf("service = %v, want trip-service", entry["service"])
	}
	if entry["component"] != "test" {
		t.Fatalf("component = %v, want test", entry["component"])
	}
	if entry["trace_id"] != span.SpanContext().TraceID().String() {
		t.Fatalf("trace_id = %v, want %s", entry["trace_id"], span.SpanContext().TraceID())
	}
	if entry["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("span_id = %v, want %s", entry["span_id"], span.SpanContext().SpanID())
	}
}

func TestNewWithoutSpanHasNoTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := logging.New(&buf, "info", "trip-service")
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.InfoContext(context.Background(), "hello")

	entry := decode(t, &buf)
	if _, ok := entry["trace_id"]; ok {
		t.Fatalf("unexpected trace_id in %v", entry)
	}
	if _, ok := entry["span_id"]; ok {
		t.Fatalf("unexpected span_id in %v", entry)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger, err := logging.New(&buf, "warn", "trip-service")
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info("hidden")
	if buf.Len() != 0 {
		t.Fatalf("info must be filtered at warn level, got %s", buf.String())
	}

	logger.Warn("shown")
	if entry := decode(t, &buf); entry["msg"] != "shown" {
		t.Fatalf("msg = %v, want shown", entry["msg"])
	}
}

func TestNewRejectsUnknownLevel(t *testing.T) {
	t.Parallel()

	if _, err := logging.New(&bytes.Buffer{}, "verbose", "trip-service"); err == nil {
		t.Fatal("expected error for unknown level")
	}
}
