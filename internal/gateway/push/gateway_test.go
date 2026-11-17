package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const acceptedBody = `{"message_id":"accepted","accepted_at":"2026-11-17T10:00:00Z"}`

func newGateway(t *testing.T, endpoint string, timeout time.Duration, options ...otelhttp.Option) *Gateway {
	t.Helper()
	gateway, err := New(endpoint, timeout, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(gateway.Close)
	return gateway
}

func TestNewRejectsInvalidSettings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		endpoint string
		timeout  time.Duration
	}{
		{name: "empty URL", timeout: time.Second},
		{name: "relative URL", endpoint: "localhost:8090", timeout: time.Second},
		{name: "unsupported scheme", endpoint: "ftp://localhost", timeout: time.Second},
		{name: "missing host", endpoint: "http://:8090", timeout: time.Second},
		{name: "malformed URL", endpoint: "http://[::1", timeout: time.Second},
		{name: "zero timeout", endpoint: "http://localhost"},
		{name: "negative timeout", endpoint: "http://localhost", timeout: -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gateway, err := New(tt.endpoint, tt.timeout)
			if err == nil {
				gateway.Close()
				t.Fatal("invalid gateway configuration accepted")
			}
		})
	}
}

func TestRequestPositionContract(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/prefix/"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			requests := make(chan map[string]any, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := requestPath
				if prefix != "" {
					wantPath = "/prefix" + requestPath
				}
				if r.Method != http.MethodPost || r.URL.Path != wantPath || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode push: %v", err)
				}
				requests <- payload
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, acceptedBody)
			}))
			t.Cleanup(server.Close)
			gateway := newGateway(t, server.URL+prefix, time.Second)
			tripID, driverID := uuid.New(), uuid.New()
			previousID := ""
			for range 2 {
				if err := gateway.RequestPosition(context.Background(), tripID, driverID); err != nil {
					t.Fatalf("RequestPosition: %v", err)
				}
				payload := <-requests
				if len(payload) != 4 || payload["kind"] != "REQUEST_POSITION" || payload["recipient_id"] != driverID.String() {
					t.Fatalf("push payload = %+v", payload)
				}
				requestID, ok := payload["request_id"].(string)
				if !ok || requestID == "" || requestID == previousID {
					t.Fatalf("request_id = %v, previous %s", payload["request_id"], previousID)
				}
				previousID = requestID
				data, ok := payload["data"].(map[string]any)
				if !ok || len(data) != 1 || data["trip_id"] != tripID.String() {
					t.Fatalf("push data = %+v", payload["data"])
				}
			}
		})
	}
}

func TestRequestPositionHTTPStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusAccepted, http.StatusOK, http.StatusNoContent, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)
			err := newGateway(t, server.URL, time.Second).RequestPosition(context.Background(), uuid.New(), uuid.New())
			if status == http.StatusAccepted {
				if err != nil {
					t.Fatalf("accepted push: %v", err)
				}
			} else {
				var statusError *HTTPError
				if !errors.As(err, &statusError) || statusError.StatusCode != status || statusError.RetryAfter != "5" {
					t.Fatalf("error = %v, want HTTP %d", err, status)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("calls = %d, want one attempt", calls.Load())
			}
		})
	}
}

func TestRequestPositionDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)
	err := newGateway(t, server.URL, time.Second).RequestPosition(context.Background(), uuid.New(), uuid.New())
	var statusError *HTTPError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusFound || calls.Load() != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestRequestPositionTimeout(t *testing.T) {
	t.Parallel()
	for _, flushHeaders := range []bool{false, true} {
		t.Run(map[bool]string{false: "response headers", true: "response body"}[flushHeaders], func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if flushHeaders {
					w.WriteHeader(http.StatusAccepted)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			err := newGateway(t, server.URL, 100*time.Millisecond).RequestPosition(context.Background(), uuid.New(), uuid.New())
			var transportError *TransportError
			if !errors.As(err, &transportError) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want transport deadline", err)
			}
		})
	}
}

func TestRequestPositionCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	gateway := newGateway(t, server.URL, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- gateway.RequestPosition(ctx, uuid.New(), uuid.New()) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("request failed before server received it: %v", err)
	}
	cancel()
	err := <-done
	var transportError *TransportError
	if !errors.As(err, &transportError) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want transport cancellation", err)
	}
}

func TestRequestPositionReusesConnection(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, acceptedBody)
	}))
	t.Cleanup(server.Close)
	gateway := newGateway(t, server.URL, time.Second)
	reused := make(chan bool, 2)
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused <- info.Reused },
	})
	for range 2 {
		if err := gateway.RequestPosition(ctx, uuid.New(), uuid.New()); err != nil {
			t.Fatal(err)
		}
	}
	if first, second := <-reused, <-reused; first || !second {
		t.Fatalf("connection reuse = %t, %t, want false, true", first, second)
	}
}

func TestGatewaySupportsConcurrentRequests(t *testing.T) {
	t.Parallel()
	const count = 12
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	gateway := newGateway(t, server.URL, time.Second)
	errorsFound := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsFound <- gateway.RequestPosition(context.Background(), uuid.New(), uuid.New())
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != count {
		t.Fatalf("calls = %d, want %d", calls.Load(), count)
	}
}

func TestRequestPositionCreatesClientSpanAndPropagatesContext(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	propagators := propagation.TraceContext{}
	received := make(chan trace.SpanContext, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagators.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		received <- trace.SpanContextFromContext(ctx)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	gateway := newGateway(t, server.URL, time.Second, otelhttp.WithTracerProvider(provider), otelhttp.WithPropagators(propagators))
	ctx, parent := provider.Tracer("push-test").Start(context.Background(), "worker")
	defer parent.End()
	if err := gateway.RequestPosition(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "POST "+requestPath || spans[0].SpanKind() != trace.SpanKindClient {
		t.Fatalf("client spans = %+v", spans)
	}
	client := spans[0]
	remote := <-received
	if client.Parent().SpanID() != parent.SpanContext().SpanID() || remote.TraceID() != client.SpanContext().TraceID() || remote.SpanID() != client.SpanContext().SpanID() || !remote.IsRemote() {
		t.Fatal("client span or propagated TraceContext does not match")
	}
}
